package lock

import "github.com/ae6rt/decap/web/api/v1"

// DistributedLockService defines the exported interface that a distributed lock service supports.
type DistributedLockService interface {
	Acquire(v1.UserBuildEvent) error
	Release(v1.UserBuildEvent) error
}

// DistributedLock models a distributed lock.  A lock has a name related to the project and branch names, and a creation time.  If an actor wishes to acquire the lock
// at a time later than Created, it is free to remove the lock and acquire it for its own use. Created is a unix time, the number of seconds since the epoch.
/*
type DistributedLock struct {
	Project string
	Branch  string
	Expires int64
}
*/