package deferrals

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/pkg/errors"
)

// SQS models the subset of exported methods we need from the greater Amazon SQS interface.
type SQS interface {
	CreateQueue(*sqs.CreateQueueInput) (*sqs.CreateQueueOutput, error)
	DeleteMessage(*sqs.DeleteMessageInput) (*sqs.DeleteMessageOutput, error)
	GetQueueAttributes(*sqs.GetQueueAttributesInput) (*sqs.GetQueueAttributesOutput, error)
	ReceiveMessage(*sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error)
	SendMessage(*sqs.SendMessageInput) (*sqs.SendMessageOutput, error)
}

// SQSDeferralService implements a deferral service on top of Amazon Simple Queue Service.
type SQSDeferralService struct {
	mutex sync.Mutex
	sqs   SQS

	queueURL string
	relay    chan<- Deferral
}

// NewSQS creates a new network client for interacting with Amazon SQS.
func NewSQS(awsAccessKey, awsAccessSecret, awsRegion string) SQS {
	sess := session.Must(session.NewSession(aws.NewConfig().WithCredentials(credentials.NewStaticCredentials(awsAccessKey, awsAccessSecret, "")).WithRegion(awsRegion).WithMaxRetries(3)))
	return sqs.New(sess)
}

// NewSQSDeferralService returns a new build deferral service based on Amazon SQS.  The write-only channel of Deferral is
// used to send Deferral events to an actor that relaunches the deferred build.  Those origin of those events
// are deferral messages on the SQS message bus.  This function creates the backing queue in SQS for use by Defer() and Resubmit().
func NewSQSDeferralService(queueName string, s SQS, r chan<- Deferral) (DeferralService, error) {
	deferralService := &SQSDeferralService{sqs: s, relay: r}
	q, err := deferralService.createQueue(queueName)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("error creating NewSQSDeferralService(%s, %+v, %v)", queueName, s, r))
	}
	deferralService.queueURL = q
	return deferralService, nil
}

// Defer defers a build based on project key and branch.  The buildID is included for reference and a deduplication device.
func (s *SQSDeferralService) Defer(projectKey, branch, buildID string) error {
	params := &sqs.SendMessageInput{
		QueueUrl:               aws.String(s.queueURL),
		MessageDeduplicationId: aws.String(buildID),
		DelaySeconds:           aws.Int64(1),
		MessageAttributes: map[string]*sqs.MessageAttributeValue{
			"projectkey": {
				DataType:    aws.String("String"),
				StringValue: aws.String(projectKey),
			},
			"branch": {
				DataType:    aws.String("String"),
				StringValue: aws.String(branch),
			},
			"buildid": {
				DataType:    aws.String("String"),
				StringValue: aws.String(buildID),
			},
			"unixtime": {
				DataType:    aws.String("Number"),
				StringValue: aws.String(fmt.Sprintf("%d", time.Now().Unix())),
			},
		},
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	_, err := s.sqs.SendMessage(params)
	return err
}

// Resubmit receives messages from the deferral queue and submits them for reexecution.  It is intended
// for this method to be called by a recurring timer.
func (s *SQSDeferralService) Resubmit() {
	params := &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(s.queueURL),
		MaxNumberOfMessages: aws.Int64(10),
		MessageAttributeNames: []*string{
			aws.String("All"),
		},
	}

	// the mutex protects the read and subsequent deletes
	s.mutex.Lock()
	defer s.mutex.Unlock()

	resp, err := s.sqs.ReceiveMessage(params)
	if err != nil {
		log.Println(err)
		return
	}

	var msgs []Deferral
	for _, message := range resp.Messages {
		t := message.MessageAttributes["unixtime"].StringValue
		unixtime, err := strconv.ParseInt(*t, 10, 64)
		if err != nil {
			log.Printf("Cannot parse unix time in Deferral:  %s\n", t)
			continue
		}

		d := Deferral{
			ProjectKey: *message.MessageAttributes["projectkey"].StringValue,
			Branch:     *message.MessageAttributes["branch"].StringValue,
			BuildID:    *message.MessageAttributes["buildid"].StringValue,
			UnixTime:   unixtime,
		}

		msgs = append(msgs, d)

		if err := s.deleteMessage(*message.ReceiptHandle); err != nil {
			log.Printf("Error deleting message %s: %v\n", *message.ReceiptHandle, err)
		}
	}

	sort.Sort(ByTime(msgs))
	msgs = dedup(msgs)
	for _, d := range msgs {
		s.relay <- d
	}
}

// DeferredBuilds returns the approximate list of deferred builds from SQS.
func (s *SQSDeferralService) DeferredBuilds() ([]Deferral, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	n, err := s.getVisibleMessagesCount()
	if err != nil {
		return nil, errors.Wrap(err, "error retrieving number of visible messages")
	}

	nMaxMessages := 10
	reads := n / nMaxMessages
	if n%10 > 0 {
		reads++
	}

	var msgs []Deferral

	for i := 0; i < reads; i++ {
		params := &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(s.queueURL),
			MaxNumberOfMessages: aws.Int64(int64(nMaxMessages)),
			MessageAttributeNames: []*string{
				aws.String("All"),
			},

			// The message should remain visible to the Resubmit goroutine.
			VisibilityTimeout: aws.Int64(0),
		}

		resp, err := s.sqs.ReceiveMessage(params)
		if err != nil {
			log.Println(err)
			continue
		}

		for _, message := range resp.Messages {
			t := message.MessageAttributes["unixtime"].StringValue
			unixtime, err := strconv.ParseInt(*t, 10, 64)
			if err != nil {
				log.Printf("Cannot parse unix time in Deferral:  %s\n", t)
				continue
			}

			d := Deferral{
				ProjectKey: *message.MessageAttributes["projectkey"].StringValue,
				Branch:     *message.MessageAttributes["branch"].StringValue,
				UnixTime:   unixtime,
				BuildID:    *message.MessageAttributes["buildid"].StringValue,
			}

			msgs = append(msgs, d)

			if err := s.deleteMessage(*message.ReceiptHandle); err != nil {
				log.Printf("Error deleting message %s: %v\n", *message.ReceiptHandle, err)
			}
		}

		sort.Sort(ByTime(msgs))
		msgs = dedup(msgs)

	}
	return msgs, nil
}

func (s *SQSDeferralService) createQueue(queueName string) (string, error) {
	params := &sqs.CreateQueueInput{
		QueueName: aws.String(queueName),
	}

	resp, err := s.sqs.CreateQueue(params)
	if err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("%T: Error creating queue: %s", err, queueName))
	}

	return *resp.QueueUrl, nil
}

func (s *SQSDeferralService) deleteMessage(handle string) error {
	params := &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(s.queueURL),
		ReceiptHandle: aws.String(handle),
	}

	_, err := s.sqs.DeleteMessage(params)

	return errors.Wrap(err, "Failed to delete message")
}

// Get the approximate number of visible messages.
func (s *SQSDeferralService) getVisibleMessagesCount() (int, error) {
	params := &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(s.queueURL),
		AttributeNames: []*string{
			aws.String("ApproximateNumberOfMessages"),
		},
	}

	resp, err := s.sqs.GetQueueAttributes(params)
	if err != nil {
		return 0, errors.Wrap(err, fmt.Sprintf("%T: Error reading available messages count: %v", s, err))
	}

	i, err := strconv.Atoi(*resp.Attributes["ApproximateNumberOfMessages"])
	if err != nil {
		return 0, err
	}

	return i, nil
}

// ByTime is used to sort Deferrals by Unix time stamp.
type ByTime []Deferral

func (s ByTime) Len() int {
	return len(s)
}
func (s ByTime) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s ByTime) Less(i, j int) bool {
	return s[i].UnixTime < s[j].UnixTime
}

func dedup(a []Deferral) []Deferral {
	var results []Deferral

	last := ""
	for _, v := range a {
		if v.Key() != last {
			results = append(results, v)
			last = v.Key()
		}
	}
	return results
}