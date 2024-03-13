package tests_test

import (
	"context"
	"flag"
	"fmt"
	"github.com/onsi/ginkgo/v2"
	ginkgo_reporters "github.com/onsi/ginkgo/v2/reporters"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"kubevirt.io/application-aware-quota/pkg/aaq-operator/resources/cluster"
	clientset "kubevirt.io/application-aware-quota/pkg/generated/aaq/clientset/versioned"
	"kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	aaqv1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
	"kubevirt.io/application-aware-quota/tests/flags"
	"kubevirt.io/application-aware-quota/tests/libaaq"
	qe_reporters "kubevirt.io/qe-tools/pkg/ginkgo-reporters"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"kubevirt.io/application-aware-quota/tests/framework"
)

const (
	pollInterval     = 2 * time.Second
	nsDeletedTimeout = 270 * time.Second
)

var (
	afterSuiteReporters = []Reporter{}
	originalAAQ         *v1alpha1.AAQ
)

var (
	kubectlPath  = flag.String("kubectl-path-aaq", "kubectl", "The path to the kubectl binary")
	ocPath       = flag.String("oc-path-aaq", "oc", "The path to the oc binary")
	aaqInstallNs = flag.String("aaq-namespace", "aaq", "The namespace of the AAQ controller")
	kubeConfig   = flag.String("kubeconfig-aaq", "/var/run/kubernetes/admin.kubeconfig", "The absolute path to the kubeconfig file")
	kubeURL      = flag.String("kubeurl", "", "kube URL url:port")
	goCLIPath    = flag.String("gocli-path-aaq", "cli.sh", "The path to cli script")
	dockerPrefix = flag.String("docker-prefix", "", "The docker host:port")
	dockerTag    = flag.String("docker-tag", "", "The docker tag")
)

// aaqFailHandler call ginkgo.Fail with printing the additional information
func aaqFailHandler(message string, callerSkip ...int) {
	if len(callerSkip) > 0 {
		callerSkip[0]++
	}
	ginkgo.Fail(message, callerSkip...)
}

func TestTests(t *testing.T) {
	defer GinkgoRecover()
	RegisterFailHandler(aaqFailHandler)
	BuildTestSuite()
	RunSpecs(t, "Tests Suite")
}

// To understand the order in which things are run, read http://onsi.github.io/ginkgo/#understanding-ginkgos-lifecycle
// flag parsing happens AFTER ginkgo has constructed the entire testing tree. So anything that uses information from flags
// cannot work when called during test tree construction.
func BuildTestSuite() {
	BeforeSuite(func() {
		fmt.Fprintf(ginkgo.GinkgoWriter, "Reading parameters\n")
		flags.NormalizeFlags()
		// Read flags, and configure client instances
		framework.ClientsInstance.KubectlPath = *kubectlPath
		framework.ClientsInstance.OcPath = *ocPath
		framework.ClientsInstance.AAQInstallNs = *aaqInstallNs
		framework.ClientsInstance.KubeConfig = *kubeConfig
		framework.ClientsInstance.KubeURL = *kubeURL
		framework.ClientsInstance.GoCLIPath = *goCLIPath
		framework.ClientsInstance.DockerPrefix = *dockerPrefix
		framework.ClientsInstance.DockerTag = *dockerTag

		fmt.Fprintf(ginkgo.GinkgoWriter, "Kubectl path: %s\n", framework.ClientsInstance.KubectlPath)
		fmt.Fprintf(ginkgo.GinkgoWriter, "OC path: %s\n", framework.ClientsInstance.OcPath)
		fmt.Fprintf(ginkgo.GinkgoWriter, "AAQ install NS: %s\n", framework.ClientsInstance.AAQInstallNs)
		fmt.Fprintf(ginkgo.GinkgoWriter, "Kubeconfig: %s\n", framework.ClientsInstance.KubeConfig)
		fmt.Fprintf(ginkgo.GinkgoWriter, "KubeURL: %s\n", framework.ClientsInstance.KubeURL)
		fmt.Fprintf(ginkgo.GinkgoWriter, "GO CLI path: %s\n", framework.ClientsInstance.GoCLIPath)
		fmt.Fprintf(ginkgo.GinkgoWriter, "DockerPrefix: %s\n", framework.ClientsInstance.DockerPrefix)
		fmt.Fprintf(ginkgo.GinkgoWriter, "DockerTag: %s\n", framework.ClientsInstance.DockerTag)

		restConfig, err := framework.ClientsInstance.LoadConfig()
		if err != nil {
			// Can't use Expect here due this being called outside of an It block, and Expect
			// requires any calls to it to be inside an It block.
			ginkgo.Fail(fmt.Sprintf("ERROR, unable to load RestConfig err:%v", err))
		}

		framework.ClientsInstance.RestConfig = restConfig
		// clients
		kcs, err := framework.ClientsInstance.GetKubeClient()
		if err != nil {
			ginkgo.Fail(fmt.Sprintf("ERROR, unable to create K8SClient: %v", err))
		}
		framework.ClientsInstance.K8sClient = kcs

		cs, err := framework.ClientsInstance.GetAaqClient()
		if err != nil {
			ginkgo.Fail(fmt.Sprintf("ERROR, unable to create AaqClient: %v", err))
		}
		framework.ClientsInstance.AaqClient = cs

		crClient, err := framework.ClientsInstance.GetCrClient()
		if err != nil {
			ginkgo.Fail(fmt.Sprintf("ERROR, unable to create CrClient: %v", err))
		}
		framework.ClientsInstance.CrClient = crClient

		dyn, err := framework.ClientsInstance.GetDynamicClient()
		if err != nil {
			ginkgo.Fail(fmt.Sprintf("ERROR, unable to create DynamicClient: %v", err))
		}
		framework.ClientsInstance.DynamicClient = dyn

		if qe_reporters.Polarion.Run {
			afterSuiteReporters = append(afterSuiteReporters, &qe_reporters.Polarion)
		}
		if qe_reporters.JunitOutput != "" {
			afterSuiteReporters = append(afterSuiteReporters, qe_reporters.NewJunitReporter())
		}

		originalAAQ, err = getRunningAAQ(framework.ClientsInstance.AaqClient)
		Expect(err).ToNot(HaveOccurred(), "cannot get the running AAQ instance")

		fmt.Fprintf(ginkgo.GinkgoWriter, "Modifying AAQ to target all namespaces\n")
		err = updateAAQNamespaceSelector(framework.ClientsInstance.AaqClient, framework.ClientsInstance.K8sClient, &metav1.LabelSelector{})
		Expect(err).ToNot(HaveOccurred(), "cannot update AAQ namespace selector")
	})

	AfterSuite(func() {
		Eventually(func() error {
			aaq, err := getRunningAAQ(framework.ClientsInstance.AaqClient)
			Expect(err).ToNot(HaveOccurred(), "cannot get the running AAQ instance")
			aaq.Spec = originalAAQ.Spec
			_, err = framework.ClientsInstance.AaqClient.AaqV1alpha1().AAQs().Update(context.Background(), aaq, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())
			return nil
		}, 60*time.Second, 1*time.Second).ShouldNot(HaveOccurred(), "waiting for aaq to be restored")
		Eventually(func() bool {
			return libaaq.AaqControllerReady(framework.ClientsInstance.K8sClient, framework.ClientsInstance.AAQInstallNs)
		}, 120*time.Second, 1*time.Second).Should(BeTrue(), "waiting for aaq controller to be ready")

		k8sClient := framework.ClientsInstance.K8sClient
		Eventually(func() []corev1.Namespace {
			nsList, _ := k8sClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{LabelSelector: framework.NsPrefixLabel})
			return nsList.Items
		}, nsDeletedTimeout, pollInterval).Should(BeEmpty())
	})

	var _ = ReportAfterSuite("TestTests", func(report Report) {
		for _, reporter := range afterSuiteReporters {
			ginkgo_reporters.ReportViaDeprecatedReporter(reporter, report)
		}
	})

}

func getRunningAAQ(aaqClient *clientset.Clientset) (*aaqv1.AAQ, error) {
	aaqList, err := aaqClient.AaqV1alpha1().AAQs().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("cannot list AAQ objects")
	}

	if len(aaqList.Items) != 1 {
		return nil, fmt.Errorf("expecting a single AAQ instance, actual: %d", len(aaqList.Items))
	}

	return aaqList.Items[0].DeepCopy(), nil
}

func updateAAQNamespaceSelector(aaqClient *clientset.Clientset, k8sClient *kubernetes.Clientset, selector *metav1.LabelSelector) error {
	// Ensure AAQ is deployed and overwrite default namespace selector to target any namespace
	aaqWebhook, err := k8sClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.Background(), cluster.MutatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	originalWebhookSelector := aaqWebhook.Webhooks[0].NamespaceSelector

	Eventually(func() error {
		aaq, err := getRunningAAQ(aaqClient)
		if err != nil {
			return err
		}

		aaq.Spec.NamespaceSelector = selector

		_, err = aaqClient.AaqV1alpha1().AAQs().Update(context.Background(), aaq, metav1.UpdateOptions{})
		return err
	}).WithTimeout(30*time.Second).WithPolling(time.Second).ShouldNot(HaveOccurred(), "cannot update AAQ object's namespace selector'")

	Eventually(func() error {
		aaqWebhook, err := k8sClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.Background(), cluster.MutatingWebhookConfigurationName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if reflect.DeepEqual(aaqWebhook.Webhooks[0].NamespaceSelector, originalWebhookSelector) {
			return fmt.Errorf("expecting namespace selector to be updated")
		}

		return nil
	}).WithTimeout(30*time.Second).WithPolling(time.Second).ShouldNot(HaveOccurred(), fmt.Sprintf("webhook namespace selector is not updated"))

	return nil
}
