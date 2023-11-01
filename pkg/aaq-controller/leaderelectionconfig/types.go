package leaderelectionconfig

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Configuration struct {
	// leaseDuration is the duration that non-leader candidates will wait
	// after observing a leadership renewal until attempting to acquire
	// leadership of a led but unrenewed leader slot. This is effectively the
	// maximum duration that a leader can be stopped before it is replaced
	// by another candidate. This is only applicable if leader election is
	// enabled.
	LeaseDuration metav1.Duration
	// renewDeadline is the interval between attempts by the acting master to
	// renew a leadership slot before it stops leading. This must be less
	// than or equal to the lease duration. This is only applicable if leader
	// election is enabled.
	RenewDeadline metav1.Duration
	// retryPeriod is the duration the clients should wait between attempting
	// acquisition and renewal of a leadership. This is only applicable if
	// leader election is enabled.
	RetryPeriod metav1.Duration
	// resourceLock indicates the resource object type that will be used to lock
	// during leader election cycles.
	ResourceLock string
}
