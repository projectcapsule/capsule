package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	certificatesv1 "k8s.io/api/certificates/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// KubeConfig represents the kubeconfig structure
type KubeConfig struct {
	APIVersion     string      `json:"apiVersion"`
	Kind           string      `json:"kind"`
	Clusters       []Cluster   `json:"clusters"`
	Contexts       []Context   `json:"contexts"`
	CurrentContext string      `json:"current-context"`
	Preferences    interface{} `json:"preferences"`
	Users          []User      `json:"users"`
}

// Cluster represents the cluster configuration
type Cluster struct {
	Name    string `json:"name"`
	Cluster struct {
		CertificateAuthorityData string `json:"certificate-authority-data"`
		Server                   string `json:"server"`
	} `json:"cluster"`
}

// Context represents the context configuration
type Context struct {
	Name    string `json:"name"`
	Context struct {
		Cluster   string `json:"cluster"`
		User      string `json:"user"`
		Namespace string `json:"namespace,omitempty"`
	} `json:"context"`
}

// User represents the user configuration
type User struct {
	Name string `json:"name"`
	User struct {
		ClientCertificateData string `json:"client-certificate-data"`
		ClientKeyData         string `json:"client-key-data"`
	} `json:"user"`
}

func createKubeconfigFile(user, tenant string, caData, certData, keyData []byte) {
	context := fmt.Sprintf("%s-%s", user, tenant)
	cluster := "orbstack"
	serverURL := "https://127.0.0.1:26443"

	kubeconfig := KubeConfig{
		APIVersion:     "v1",
		Kind:           "Config",
		CurrentContext: context,
		Clusters: []Cluster{
			{
				Name: cluster,
				Cluster: struct {
					CertificateAuthorityData string `json:"certificate-authority-data"`
					Server                   string `json:"server"`
				}{
					CertificateAuthorityData: base64.StdEncoding.EncodeToString(caData),
					Server:                   serverURL,
				},
			},
		},
		Contexts: []Context{
			{
				Name: context,
				Context: struct {
					Cluster   string `json:"cluster"`
					User      string `json:"user"`
					Namespace string `json:"namespace,omitempty"`
				}{
					Cluster: cluster,
					User:    user,
				},
			},
		},
		Users: []User{
			{
				Name: user,
				User: struct {
					ClientCertificateData string `json:"client-certificate-data"`
					ClientKeyData         string `json:"client-key-data"`
				}{
					ClientCertificateData: base64.StdEncoding.EncodeToString(certData),
					ClientKeyData:         base64.StdEncoding.EncodeToString(keyData),
				},
			},
		},
	}

	// Embed CA certificate data directly
	kubeconfig.Clusters[0].Cluster.CertificateAuthorityData = string(caData)

	kubeconfigJSON, err := json.MarshalIndent(kubeconfig, "", "  ")
	if err != nil {
		fmt.Println("Error marshaling kubeconfig to JSON:", err)
		os.Exit(1)
	}

	kubeconfigFileName := fmt.Sprintf("%s-%s.kubeconfig", user, tenant)
	err = ioutil.WriteFile(kubeconfigFileName, kubeconfigJSON, 0644)
	if err != nil {
		fmt.Println("Error creating kubeconfig file:", err)
		os.Exit(1)
	}

	fmt.Printf("kubeconfig file is: %s\n", kubeconfigFileName)
	fmt.Printf("To use it as %s, export KUBECONFIG=%s\n", user, kubeconfigFileName)
}

func fetchCertificate(clientset *kubernetes.Clientset, user, tenant string) {
	csrObj, err := clientset.CertificatesV1().CertificateSigningRequests().Get(context.TODO(), fmt.Sprintf("%s-%s", user, tenant), metav1.GetOptions{})
	if err != nil {
		fmt.Println("Error getting CSR object:", err)
		os.Exit(1)
	}

	//err = wait.PollImmediate(time.Second, time.Minute*0, func() (bool, error) {
	if len(csrObj.Status.Certificate) > 0 {
		//return true, nil
	}
	csrObj, err = clientset.CertificatesV1().CertificateSigningRequests().Get(context.TODO(), fmt.Sprintf("%s-%s", user, tenant), metav1.GetOptions{})
	if err != nil {
		fmt.Println("Error getting CSR object:", err)
		os.Exit(1)
	}

	certData, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(csrObj.Status.Certificate)))
	if err != nil {
		fmt.Println("Error decoding certificate data:", err)
		os.Exit(1)
	}

	err = ioutil.WriteFile(fmt.Sprintf("%s-%s.crt", user, tenant), certData, 0644)
	if err != nil {
		fmt.Println("Error writing signed certificate to file:", err)
		os.Exit(1)
	}
}

func main() {

	user := "oil"
	tenant := "oil"
	group := ""

	if user == "" || tenant == "" {
		fmt.Println("User and Tenant must be specified!")
		os.Exit(1)
	}

	if group == "" {
		group = "capsule.clastix.io"
	}

	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	if err != nil {
		fmt.Println("Error building Kubernetes client config:", err)
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Println("Error creating Kubernetes client:", err)
		os.Exit(1)
	}

	fmt.Printf("Creating certs in memory\n")

	mergedGroups := fmt.Sprintf("/O=%s", group)
	mergedGroups = strings.ReplaceAll(mergedGroups, ",", "/O=")
	fmt.Printf("Merging groups %s\n", mergedGroups)

	key, csrDER, err := generateKeyAndCSR(user, mergedGroups)
	if err != nil {
		fmt.Println("Error generating key and CSR:", err)
		os.Exit(1)
	}

	// Save CSR to CertificateSigningRequest object
	_, err = createCSRObject(clientset, user, tenant, csrDER)
	if err != nil {
		fmt.Println("Error creating CSR object:", err)
		os.Exit(1)
	}

	approveCSR(clientset, user, tenant)

	fetchCertificate(clientset, user, tenant)

	// Updated code to get CA certificate
	var caData []byte

	// Check if TLSClientConfig is not nil
	if config.TLSClientConfig.CAFile != "" || config.TLSClientConfig.CertFile != "" || config.TLSClientConfig.KeyFile != "" {
		// Extract CA certificate from the Kubernetes config file
		caFile := config.TLSClientConfig.CAFile
		caData, err = ioutil.ReadFile(caFile)
		if err != nil {
			fmt.Println("Error reading cluster CA certificate:", err)
			os.Exit(1)
		}
	} else if len(config.TLSClientConfig.CAData) > 0 {
		// Use CA data directly if provided in the config
		caData = config.TLSClientConfig.CAData
	} else {
		fmt.Println("CA certificate information not found in the Kubernetes config")
		os.Exit(1)
	}

	// Base64 encode CA data
	encodedCAData := base64.StdEncoding.EncodeToString(caData)

	// Convert the private key to a byte slice
	keyBytes := x509.MarshalPKCS1PrivateKey(key)

	// Base64 encode key data
	encodedKeyData := base64.StdEncoding.EncodeToString(keyBytes)

	csrObj, _ := clientset.CertificatesV1().CertificateSigningRequests().Get(context.TODO(), fmt.Sprintf("%s-%s", user, tenant), metav1.GetOptions{})

	fmt.Println(csrObj)

	encodedCertificate := base64.StdEncoding.EncodeToString(csrObj.Status.Certificate)

	createKubeconfigFile(user, tenant, []byte(encodedCAData), []byte(encodedCertificate), []byte(encodedKeyData))
}

// generateKeyAndCSR generates a new RSA private key and a Certificate Signing Request (CSR).
func generateKeyAndCSR(user, mergedGroups string) (*rsa.PrivateKey, []byte, error) {
	// Generate RSA private key
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	// Generate CSR
	template := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   fmt.Sprintf("%s%s", user, mergedGroups),
			Organization: []string{"system:authenticated"},
		},
		SignatureAlgorithm: x509.SHA256WithRSA,
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &template, key)
	if err != nil {
		return nil, nil, err
	}

	return key, csrDER, nil
}

func createCSRObject(clientset *kubernetes.Clientset, user, tenant string, csrDER []byte) (*certificatesv1.CertificateSigningRequest, error) {
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	csrObj := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s", user, tenant),
		},
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Groups:     []string{"system:authenticated"},
			Request:    csrPEM,
			SignerName: "kubernetes.io/kube-apiserver-client",
			Usages: []certificatesv1.KeyUsage{
				certificatesv1.UsageDigitalSignature,
				certificatesv1.UsageKeyEncipherment,
				certificatesv1.UsageClientAuth,
			},
		},
	}

	return clientset.CertificatesV1().CertificateSigningRequests().Create(context.TODO(), csrObj, metav1.CreateOptions{})
}

func approveCSR(clientset *kubernetes.Clientset, user, tenant string) {
	err := wait.PollImmediate(time.Second, time.Second*10, func() (bool, error) {
		csrObj, _ := clientset.CertificatesV1().CertificateSigningRequests().Get(context.TODO(), fmt.Sprintf("%s-%s", user, tenant), metav1.GetOptions{})

		fmt.Println(csrObj)

		csrObj.Status = certificatesv1.CertificateSigningRequestStatus{
			Conditions: append(csrObj.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
				Type:           certificatesv1.CertificateApproved,
				Reason:         "KubectlApprove",
				Status:         "True",
				LastUpdateTime: metav1.Now(),
			}),
		}

		_, err := clientset.CertificatesV1().CertificateSigningRequests().UpdateApproval(context.TODO(), fmt.Sprintf("%s-%s", user, tenant), csrObj, metav1.UpdateOptions{})
		if err != nil {
			fmt.Println("Error approving the CSR:", err)
			os.Exit(1)
		}

		return true, nil
	})

	if err != nil {
		fmt.Println("Error waiting for CSR approval:", err)
		os.Exit(1)
	}
}