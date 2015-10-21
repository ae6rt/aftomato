"use strict";angular.module("decapApp").directive("buildQueue",["DecapService",function(DecapService){return{templateUrl:"scripts/directives/dashboard/buildQueue/buildQueue.html",restrict:"E",replace:!0,scope:{model:"="},controller:function($scope,Notification){$scope.bar={series:["Builds","Branches"],labels:[],data:[[],[]]},$scope.notifyMe=function(){Notification("Feature coming soon!")},DecapService.getQueueStatus().then(function(statusInfo){$scope.queueState=statusInfo.state,$scope.alertColor="open"===$scope.queueState?"success":"warning",$scope.glyphIcon="open"===$scope.queueState?"glyphicon-arrow-up":"glyphicon-arrow-down",DecapService.getBuilds("ae6rt","hello-world-java").then(function(builds){for(var i in builds){for(var b in builds)builds[b].timestamp=new Date(1e3*builds[b].startTime).toISOString();$scope.builds=builds}},function(message){console.log("error getting buildQueue->getBuilds: "+message)})},function(message){console.log("error getting buildQueue->statusInfo: "+message)})}}}]);