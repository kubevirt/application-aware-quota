package utils

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	aaqclientset "kubevirt.io/application-aware-quota/pkg/generated/aaq/clientset/versioned"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"time"
)

const (
	// how long to wait for a Application Aware Resource Quota update to occur
	resourceQuotaTimeout = 2 * time.Minute
)

// CreateApplicationAwareClusterResourceQuota in the specified namespace
func CreateApplicationAwareClusterResourceQuota(ctx context.Context, c *aaqclientset.Clientset, ApplicationAwareClusterResourceQuota *v1alpha1.ApplicationAwareClusterResourceQuota) (*v1alpha1.ApplicationAwareClusterResourceQuota, error) {
	return c.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Create(ctx, ApplicationAwareClusterResourceQuota, metav1.CreateOptions{})
}

// DeleteApplicationAwareClusterResourceQuota with the specified name
func DeleteApplicationAwareClusterResourceQuota(ctx context.Context, c *aaqclientset.Clientset, name string) error {
	return c.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Delete(ctx, name, metav1.DeleteOptions{})
}

// wait for Application Aware Cluster Resource Quota status to show the expected used resources value
func WaitForApplicationAwareClusterResourceQuota(ctx context.Context, c *aaqclientset.Clientset, quotaName string, used v1.ResourceList) error {
	return wait.PollWithContext(ctx, 2*time.Second, resourceQuotaTimeout, func(ctx context.Context) (bool, error) {
		ApplicationAwareResourceQuota, err := c.AaqV1alpha1().ApplicationAwareClusterResourceQuotas().Get(ctx, quotaName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		// used may not yet be calculated
		if ApplicationAwareResourceQuota.Status.Total.Used == nil {
			return false, nil
		}
		// verify that the quota shows the expected used resource values
		for k, v := range used {
			if actualValue, found := ApplicationAwareResourceQuota.Status.Total.Used[k]; !found || (actualValue.Cmp(v) != 0) {
				fmt.Printf(fmt.Sprintf("resource %s, expected %s, actual %s\n", k, v.String(), actualValue.String()))
				return false, nil
			}
		}
		return true, nil
	})
}
