package tests_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubevirt.io/application-aware-quota/tests/framework"
)

const (
	annNotAfter  = "auth.openshift.io/certificate-not-after"
	annNotBefore = "auth.openshift.io/certificate-not-before"
)

var _ = Describe("Cert rotation tests", func() {
	f := framework.NewFramework("certrotation-test")

	Context("with port forward", func() {
		var cmd *exec.Cmd

		AfterEach(func() {
			afterCMD(cmd)
		})

		It("check secret re read", func() {
			serviceName := "aaq-server"
			secretName := "aaq-server-cert"
			var (
				err      error
				hostPort string
			)
			hostPort, cmd, err = startServicePortForward(f, serviceName)
			Expect(err).ToNot(HaveOccurred())

			var conn *tls.Conn

			Eventually(func() error {
				conn, err = tls.Dial("tcp", hostPort, &tls.Config{
					InsecureSkipVerify: true,
				})
				return err
			}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

			oldExpire := conn.ConnectionState().PeerCertificates[0].NotAfter
			err = conn.Close()
			Expect(err).ToNot(HaveOccurred())

			rotateCert(f, secretName)

			Eventually(func() error {
				conn, err = tls.Dial("tcp", hostPort, &tls.Config{
					InsecureSkipVerify: true,
				})

				if err != nil {
					return err
				}

				defer func(conn *tls.Conn) {
					err := conn.Close()
					Expect(err).ToNot(HaveOccurred())
				}(conn)

				if conn.ConnectionState().PeerCertificates[0].NotAfter.After(oldExpire) {
					return nil
				}

				return fmt.Errorf("Expire time not updated")

			}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

		})
	})

	It("check secret updated", func() {
		configMapName := "aaq-server-signer-bundle"
		secretName := "aaq-server"
		var oldBundle *corev1.ConfigMap
		oldSecret, err := f.K8sClient.CoreV1().Secrets(f.AAQInstallNs).Get(context.TODO(), secretName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		oldBundle, err = f.K8sClient.CoreV1().ConfigMaps(f.AAQInstallNs).Get(context.TODO(), configMapName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		rotateCert(f, secretName)

		Eventually(func() bool {
			updatedSecret, err := f.K8sClient.CoreV1().Secrets(f.AAQInstallNs).Get(context.TODO(), secretName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			return updatedSecret.Annotations[annNotAfter] != oldSecret.Annotations[annNotAfter] &&
				string(updatedSecret.Data["tls.cert"]) != string(oldSecret.Data["tls.crt"]) &&
				string(updatedSecret.Data["tls.key"]) != string(oldSecret.Data["tls.key"])

		}, 60*time.Second, 1*time.Second).Should(BeTrue())

		Eventually(func() error {
			updatedBundle, err := f.K8sClient.CoreV1().ConfigMaps(f.AAQInstallNs).Get(context.TODO(), configMapName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			if updatedBundle.Data["ca-bundle.crt"] == oldBundle.Data["ca-bundle.crt"] {
				return fmt.Errorf("bundle data should be updated")
			}
			return nil
		}, 60*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

	})
})

func rotateCert(f *framework.Framework, secretName string) {
	secret, err := f.K8sClient.CoreV1().Secrets(f.AAQInstallNs).Get(context.TODO(), secretName, metav1.GetOptions{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	nb, ok := secret.Annotations[annNotBefore]
	ExpectWithOffset(1, ok).To(BeTrue())

	notBefore, err := time.Parse(time.RFC3339, nb)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	ExpectWithOffset(1, time.Since(notBefore).Seconds()).To(BeNumerically(">", 0))

	newSecret := secret.DeepCopy()
	newSecret.Annotations[annNotAfter] = notBefore.Add(time.Second).Format(time.RFC3339)

	_, err = f.K8sClient.CoreV1().Secrets(f.AAQInstallNs).Update(context.TODO(), newSecret, metav1.UpdateOptions{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
}

func startServicePortForward(f *framework.Framework, serviceName string) (string, *exec.Cmd, error) {
	lp := "18443"
	pm := lp + ":443"
	hostPort := "127.0.0.1:" + lp

	cmd := f.CreateKubectlCommand("-n", f.AAQInstallNs, "port-forward", "svc/"+serviceName, pm)
	err := cmd.Start()
	if err != nil {
		return "", nil, err
	}

	return hostPort, cmd, nil
}

func afterCMD(cmd *exec.Cmd) {
	if cmd != nil {
		ExpectWithOffset(1, cmd.Process.Kill()).Should(Succeed())
		err := cmd.Wait()
		if err != nil {
			_, ok := err.(*exec.ExitError)
			ExpectWithOffset(1, ok).Should(BeTrue())
		}
	}
}
