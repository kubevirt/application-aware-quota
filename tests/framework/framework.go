package framework

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	clientset "kubevirt.io/application-aware-quota/pkg/generated/aaq/clientset/versioned"
	virtclientset "kubevirt.io/application-aware-quota/pkg/generated/kubevirt/clientset/versioned"

	"net/http"

	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	aaqclientset "kubevirt.io/application-aware-quota/pkg/generated/aaq/clientset/versioned"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	aaqv1 "kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1"
)

const (
	// PrometheusLabelKey provides the label to indicate prometheus metrics are available in the pods.
	PrometheusLabelKey = "prometheus.aaq.kubevirt.io"
	// PrometheusLabelValue provides the label value which shouldn't be empty to avoid a prometheus WIP issue.
	PrometheusLabelValue = "true"
	// HonorWaitForFirstConsumer - if enabled will not schedule worker pods on a storage with WaitForFirstConsumer binding mode
	HonorWaitForFirstConsumer = "HonorWaitForFirstConsumer"
	nsCreateTime              = 60 * time.Second
	//NsPrefixLabel provides a aaq prefix label to identify the test namespace
	NsPrefixLabel   = "aaq-e2e"
	timeout         = time.Second * 90
	pollingInterval = time.Second
)

// run-time flags
var (
	ClientsInstance = &Clients{}
	reporter        = NewKubernetesReporter()
)

// Config provides some basic test config options
type Config struct {
	// SkipNamespaceCreation sets whether to skip creating a namespace. Use this ONLY for tests that do not require
	// a namespace at all, like basic sanity or other global tests.
	SkipNamespaceCreation bool

	// FeatureGates may be overridden for a framework
	FeatureGates []string
}

// Clients is the struct containing the client-go kubernetes clients
type Clients struct {
	KubectlPath  string
	OcPath       string
	AAQInstallNs string
	KubeConfig   string
	KubeURL      string
	GoCLIPath    string
	DockerPrefix string
	DockerTag    string

	//  k8sClient provides our k8s client pointer
	K8sClient *kubernetes.Clientset
	// AaqClient provides our AAQ client pointer
	AaqClient *aaqclientset.Clientset
	// CrClient is a controller runtime client
	CrClient crclient.Client
	// RestConfig provides a pointer to our REST client config.
	RestConfig *rest.Config
	// DynamicClient performs generic operations on arbitrary k8s API objects.
	DynamicClient dynamic.Interface
}

// Framework supports common operations used by functional/e2e tests. It holds the k8s and AAQ clients,
// a generated unique namespace, run-time flags, and more fields will be added over time as AAQ e2e
// evolves. Global BeforeEach and AfterEach are called in the Framework constructor.
type Framework struct {
	Config
	// NsPrefix is a prefix for generated namespace
	NsPrefix string
	// Namespace provides a namespace for each test generated/unique ns per test
	Namespace          *v1.Namespace
	namespacesToDelete []*v1.Namespace

	*Clients
	reporter *KubernetesReporter
}

// NewFramework calls NewFramework and handles errors by calling Fail. Config is optional, but
// if passed there can only be one.
// To understand the order in which things are run, read http://onsi.github.io/ginkgo/#understanding-ginkgos-lifecycle
// flag parsing happens AFTER ginkgo has constructed the entire testing tree. So anything that uses information from flags
// cannot work when called during test tree construction.
func NewFramework(prefix string, config ...Config) *Framework {
	cfg := Config{
		FeatureGates: []string{HonorWaitForFirstConsumer},
	}
	if len(config) > 0 {
		cfg = config[0]
	}
	f := &Framework{
		Config:   cfg,
		NsPrefix: prefix,
		Clients:  ClientsInstance,
		reporter: reporter,
	}

	ginkgo.BeforeEach(f.BeforeEach)
	ginkgo.AfterEach(f.AfterEach)
	return f
}

// BeforeEach provides a set of operations to run before each test
func (f *Framework) BeforeEach() {
	if !f.SkipNamespaceCreation {
		// generate unique primary ns (ns2 not created here)
		ginkgo.By(fmt.Sprintf("Building a %q namespace api object", f.NsPrefix))
		ns, err := f.CreateNamespace(f.NsPrefix, map[string]string{
			NsPrefixLabel: f.NsPrefix,
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		f.Namespace = ns
		f.AddNamespaceToDelete(ns)
	}
}

// AfterEach provides a set of operations to run after each test
func (f *Framework) AfterEach() {
	// delete the namespace(s) in a defer in case future code added here could generate
	// an exception. For now there is only a defer.
	defer func() {
		for _, ns := range f.namespacesToDelete {
			defer func() { f.namespacesToDelete = nil }()
			if ns == nil || len(ns.Name) == 0 {
				continue
			}
			ginkgo.By(fmt.Sprintf("Destroying namespace %q for this suite.", ns.Name))
			err := DeleteNS(f.K8sClient, ns.Name)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

	}()
	if ginkgo.CurrentSpecReport().Failed() {
		f.reporter.FailureCount++
		fmt.Fprintf(ginkgo.GinkgoWriter, "On failure, artifacts will be collected in %s/%d_*\n", f.reporter.artifactsDir, f.reporter.FailureCount)
		f.reporter.Dump(f.K8sClient, ginkgo.CurrentSpecReport().RunTime)
	}
}

// CreateNamespace instantiates a new namespace object with a unique name and the passed-in label(s).
func (f *Framework) CreateNamespace(prefix string, labels map[string]string) (*v1.Namespace, error) {
	if labels == nil {
		labels = make(map[string]string)
	}

	if IsOpenshift(f.K8sClient) {
		labels["security.openshift.io/scc.podSecurityLabelSync"] = "false"
	}

	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("aaq-e2e-tests-%s-", prefix),
			Namespace:    "",
			Labels:       labels,
		},
		Status: v1.NamespaceStatus{},
	}

	var nsObj *v1.Namespace
	c := f.K8sClient
	err := wait.PollImmediate(2*time.Second, nsCreateTime, func() (bool, error) {
		var err error
		nsObj, err = c.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		if err == nil || apierrs.IsAlreadyExists(err) {
			return true, nil // done
		}
		klog.Warningf("Unexpected error while creating %q namespace: %v", ns.GenerateName, err)
		return false, err // keep trying
	})
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(ginkgo.GinkgoWriter, "INFO: Created new namespace %q\n", nsObj.Name)
	return nsObj, nil
}

// AddNamespaceToDelete provides a wrapper around the go append function
func (f *Framework) AddNamespaceToDelete(ns *v1.Namespace) {
	f.namespacesToDelete = append(f.namespacesToDelete, ns)
}

// DeleteNS provides a function to delete the specified namespace from the test cluster
func DeleteNS(c *kubernetes.Clientset, ns string) error {
	// return wait.PollImmediate(2*time.Second, nsDeleteTime, func() (bool, error) {
	err := c.CoreV1().Namespaces().Delete(context.TODO(), ns, metav1.DeleteOptions{})
	if err != nil && !apierrs.IsNotFound(err) {
		return err
	}
	return nil
}

// GetDynamicClient gets an instance of a dynamic client that performs generic operations on arbitrary k8s API objects.
func (c *Clients) GetDynamicClient() (dynamic.Interface, error) {
	cfg, err := clientcmd.BuildConfigFromFlags(c.KubeURL, c.KubeConfig)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return dyn, nil
}

func (c *Clients) GetVirtClient() (*virtclientset.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags(c.KubeURL, c.KubeConfig)
	if err != nil {
		return nil, err
	}
	vc, err := virtclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return vc, nil
}

// GetMtqClient gets an instance of a kubernetes client that includes all the MTQ extensions.
func (c *Clients) GetAaqClient() (*clientset.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags(c.KubeURL, c.KubeConfig)
	if err != nil {
		return nil, err
	}
	aaqClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return aaqClient, nil
}

// GetCrClient returns a controller runtime client
func (c *Clients) GetCrClient() (crclient.Client, error) {

	if err := aaqv1.AddToScheme(scheme.Scheme); err != nil {
		return nil, err
	}
	client, err := crclient.New(c.RestConfig, crclient.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, err
	}

	return client, nil
}

// GetRESTConfig returns a RESTConfig
func (f *Framework) GetRESTConfig(namespace, name string) (*rest.Config, error) {
	token, err := f.GetToken(namespace, name)
	if err != nil {
		return nil, err
	}

	return &rest.Config{
		Host:        f.RestConfig.Host,
		APIPath:     f.RestConfig.APIPath,
		BearerToken: string(token),
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}, nil
}

// GetToken returns a token for a given SA
func (f *Framework) GetToken(namespace, name string) (string, error) {
	token, err := f.K8sClient.CoreV1().ServiceAccounts(namespace).
		CreateToken(
			context.TODO(),
			name,
			&authenticationv1.TokenRequest{
				Spec: authenticationv1.TokenRequestSpec{},
			},
			metav1.CreateOptions{},
		)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(ginkgo.GinkgoWriter, "INFO: Token created for SA: %+v\n", token.Status)

	return token.Status.Token, nil
}

// GetKubeClient returns a Kubernetes rest client
func (c *Clients) GetKubeClient() (*kubernetes.Clientset, error) {
	return GetKubeClientFromRESTConfig(c.RestConfig)
}

// LoadConfig loads our specified kubeconfig
func (c *Clients) LoadConfig() (*rest.Config, error) {
	return clientcmd.BuildConfigFromFlags(c.KubeURL, c.KubeConfig)
}

// GetKubeClientFromRESTConfig provides a function to get a K8s client using hte REST config
func GetKubeClientFromRESTConfig(config *rest.Config) (*kubernetes.Clientset, error) {
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	return kubernetes.NewForConfig(config)
}

// CreatePrometheusServiceInNs creates a service for prometheus in the specified namespace. This
// allows us to test for prometheus end points using the service to connect to the endpoints.
func (f *Framework) CreatePrometheusServiceInNs(namespace string) (*v1.Service, error) {
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubevirt-prometheus-metrics",
			Namespace: namespace,
			Labels: map[string]string{
				PrometheusLabelKey: PrometheusLabelValue,
				"kubevirt.io":      "",
			},
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name: "metrics",
					Port: 8443,
					TargetPort: intstr.IntOrString{
						StrVal: "metrics",
					},
					Protocol: v1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				PrometheusLabelKey: PrometheusLabelValue,
			},
		},
	}
	return f.K8sClient.CoreV1().Services(namespace).Create(context.TODO(), service, metav1.CreateOptions{})
}

// ExpectEvent polls and fetches events during a defined period of time
func (f *Framework) ExpectEvent(aaqNamespace string) gomega.AsyncAssertion {
	return gomega.Eventually(func() string {
		events, err := f.runKubectlCommand("get", "events", "-n", aaqNamespace)
		if err == nil {
			fmt.Fprintf(ginkgo.GinkgoWriter, "%s", events)
			return events
		}
		fmt.Fprintf(ginkgo.GinkgoWriter, "ERROR: %s\n", err.Error())
		return ""
	}, timeout, pollingInterval)
}

// runKubectlCommand ...
func (f *Framework) runKubectlCommand(args ...string) (string, error) {
	var errb bytes.Buffer
	cmd := f.createKubectlCommand(args...)

	cmd.Stderr = &errb
	stdOutBytes, err := cmd.Output()
	if err != nil {
		if len(errb.String()) > 0 {
			return errb.String(), err
		}
	}
	return string(stdOutBytes), nil
}

// createKubectlCommand returns the Cmd to execute kubectl
func (f *Framework) createKubectlCommand(args ...string) *exec.Cmd {
	kubeconfig := f.KubeConfig
	path := f.KubectlPath

	cmd := exec.Command(path, args...)
	kubeconfEnv := fmt.Sprintf("KUBECONFIG=%s", kubeconfig)
	cmd.Env = append(os.Environ(), kubeconfEnv)

	return cmd
}

func getMaxFailsFromEnv() int {
	maxFailsEnv := os.Getenv("REPORTER_MAX_FAILS")
	if maxFailsEnv == "" {
		fmt.Fprintf(os.Stderr, "defaulting to 10 reported failures\n")
		return 10
	}

	maxFails, err := strconv.Atoi(maxFailsEnv)
	if err != nil { // if the variable is set with a non int value
		fmt.Println("Invalid REPORTER_MAX_FAILS variable, defaulting to 10")
		return 10
	}

	fmt.Fprintf(os.Stderr, "Number of reported failures[%d]\n", maxFails)
	return maxFails
}

// KubernetesReporter is the struct that holds the report info.
type KubernetesReporter struct {
	FailureCount int
	artifactsDir string
	maxFails     int
}

// NewKubernetesReporter creates a new instance of the reporter.
func NewKubernetesReporter() *KubernetesReporter {
	return &KubernetesReporter{
		FailureCount: 0,
		artifactsDir: os.Getenv("ARTIFACTS"),
		maxFails:     getMaxFailsFromEnv(),
	}
}

// Dump dumps the current state of the cluster. The relevant logs are collected starting
// from the since parameter.
func (r *KubernetesReporter) Dump(kubeCli *kubernetes.Clientset, since time.Duration) {
	// If we got not directory, print to stderr
	if r.artifactsDir == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "Current failure count[%d]\n", r.FailureCount)
	if r.FailureCount > r.maxFails {
		return
	}

	// Can call this as many times as needed, if the directory exists, nothing happens.
	if err := os.MkdirAll(r.artifactsDir, 0777); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create directory: %v\n", err)
		return
	}

	r.logEvents(kubeCli, since)
	r.logNodes(kubeCli)
	r.logPVCs(kubeCli)
	r.logPVs(kubeCli)
	r.logPods(kubeCli)
	r.logServices(kubeCli)
	r.logEndpoints(kubeCli)
	r.logLogs(kubeCli, since)
}

// Cleanup cleans up the current content of the artifactsDir
func (r *KubernetesReporter) Cleanup() {
	// clean up artifacts from previous run
	if r.artifactsDir != "" {
		os.RemoveAll(r.artifactsDir)
	}
}

func (r *KubernetesReporter) logPods(kubeCli *kubernetes.Clientset) {
	f, err := os.OpenFile(filepath.Join(r.artifactsDir, fmt.Sprintf("%d_pods.log", r.FailureCount)),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open the file: %v", err)
		return
	}
	defer f.Close()

	pods, err := kubeCli.CoreV1().Pods(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch pods: %v\n", err)
		return
	}

	j, err := json.MarshalIndent(pods, "", "    ")
	if err != nil {
		return
	}
	fmt.Fprintln(f, string(j))
}

func (r *KubernetesReporter) logServices(kubeCli *kubernetes.Clientset) {
	f, err := os.OpenFile(filepath.Join(r.artifactsDir, fmt.Sprintf("%d_services.log", r.FailureCount)),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open the file: %v", err)
		return
	}
	defer f.Close()

	services, err := kubeCli.CoreV1().Services(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch services: %v\n", err)
		return
	}

	j, err := json.MarshalIndent(services, "", "    ")
	if err != nil {
		return
	}
	fmt.Fprintln(f, string(j))
}

func (r *KubernetesReporter) logEndpoints(kubeCli *kubernetes.Clientset) {
	f, err := os.OpenFile(filepath.Join(r.artifactsDir, fmt.Sprintf("%d_endpoints.log", r.FailureCount)),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open the file: %v", err)
		return
	}
	defer f.Close()

	endpoints, err := kubeCli.CoreV1().Endpoints(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch endpointss: %v\n", err)
		return
	}

	j, err := json.MarshalIndent(endpoints, "", "    ")
	if err != nil {
		return
	}
	fmt.Fprintln(f, string(j))
}

func (r *KubernetesReporter) logNodes(kubeCli *kubernetes.Clientset) {
	f, err := os.OpenFile(filepath.Join(r.artifactsDir, fmt.Sprintf("%d_nodes.log", r.FailureCount)),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open the file: %v\n", err)
		return
	}
	defer f.Close()

	nodes, err := kubeCli.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch nodes: %v\n", err)
		return
	}

	j, err := json.MarshalIndent(nodes, "", "    ")
	if err != nil {
		return
	}
	fmt.Fprintln(f, string(j))
}

func (r *KubernetesReporter) logPVs(kubeCli *kubernetes.Clientset) {
	f, err := os.OpenFile(filepath.Join(r.artifactsDir, fmt.Sprintf("%d_pvs.log", r.FailureCount)),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open the file: %v\n", err)
		return
	}
	defer f.Close()

	pvs, err := kubeCli.CoreV1().PersistentVolumes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch pvs: %v\n", err)
		return
	}

	j, err := json.MarshalIndent(pvs, "", "    ")
	if err != nil {
		return
	}
	fmt.Fprintln(f, string(j))
}

func (r *KubernetesReporter) logPVCs(kubeCli *kubernetes.Clientset) {
	f, err := os.OpenFile(filepath.Join(r.artifactsDir, fmt.Sprintf("%d_pvcs.log", r.FailureCount)),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open the file: %v\n", err)
		return
	}
	defer f.Close()

	pvcs, err := kubeCli.CoreV1().PersistentVolumeClaims(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch pvcs: %v\n", err)
		return
	}

	j, err := json.MarshalIndent(pvcs, "", "    ")
	if err != nil {
		return
	}
	fmt.Fprintln(f, string(j))
}

func (r *KubernetesReporter) logLogs(kubeCli *kubernetes.Clientset, since time.Duration) {
	logsdir := filepath.Join(r.artifactsDir, "pods")

	if err := os.MkdirAll(logsdir, 0777); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create directory: %v\n", err)
		return
	}

	startTime := time.Now().Add(-since).Add(-5 * time.Second)

	pods, err := kubeCli.CoreV1().Pods(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch pods: %v\n", err)
		return
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			current, err := os.OpenFile(filepath.Join(logsdir, fmt.Sprintf("%d_%s_%s-%s.log", r.FailureCount, pod.Namespace, pod.Name, container.Name)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to open the file: %v\n", err)
				return
			}
			defer current.Close()

			previous, err := os.OpenFile(filepath.Join(logsdir, fmt.Sprintf("%d_%s_%s-%s_previous.log", r.FailureCount, pod.Namespace, pod.Name, container.Name)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to open the file: %v\n", err)
				return
			}
			defer previous.Close()

			logStart := metav1.NewTime(startTime)
			logs, err := kubeCli.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &v1.PodLogOptions{SinceTime: &logStart, Container: container.Name}).DoRaw(context.TODO())
			if err == nil {
				fmt.Fprintln(current, string(logs))
			}

			logs, err = kubeCli.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &v1.PodLogOptions{SinceTime: &logStart, Container: container.Name, Previous: true}).DoRaw(context.TODO())
			if err == nil {
				fmt.Fprintln(previous, string(logs))
			}
		}
	}
}

func (r *KubernetesReporter) logEvents(kubeCli *kubernetes.Clientset, since time.Duration) {
	f, err := os.OpenFile(filepath.Join(r.artifactsDir, fmt.Sprintf("%d_events.log", r.FailureCount)),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open the file: %v\n", err)
		return
	}
	defer f.Close()

	startTime := time.Now().Add(-since).Add(-5 * time.Second)

	events, err := kubeCli.CoreV1().Events(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return
	}

	e := events.Items
	sort.Slice(e, func(i, j int) bool {
		return e[i].LastTimestamp.After(e[j].LastTimestamp.Time)
	})

	eventsToPrint := v1.EventList{}
	for _, event := range e {
		if event.LastTimestamp.Time.After(startTime) {
			eventsToPrint.Items = append(eventsToPrint.Items, event)
		}
	}

	j, err := json.MarshalIndent(eventsToPrint, "", "    ")
	if err != nil {
		return
	}
	fmt.Fprintln(f, string(j))
}

// IsOpenshift checks if we are on OpenShift platform
func IsOpenshift(client kubernetes.Interface) bool {
	//OpenShift 3.X check
	result := client.Discovery().RESTClient().Get().AbsPath("/oapi/v1").Do(context.TODO())
	var statusCode int
	result.StatusCode(&statusCode)

	if result.Error() == nil {
		// It is OpenShift
		if statusCode == http.StatusOK {
			return true
		}
	} else {
		// Got 404 so this is not Openshift 3.X, let's check OpenShift 4
		result = client.Discovery().RESTClient().Get().AbsPath("/apis/route.openshift.io").Do(context.TODO())
		var statusCode int
		result.StatusCode(&statusCode)

		if result.Error() == nil {
			// It is OpenShift
			if statusCode == http.StatusOK {
				return true
			}
		}
	}

	return false
}
