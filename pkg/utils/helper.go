package utils

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type CommandOutputStreams struct {
	Stdout string
	Stderr string
}

// GetHostName get host name
func GetHostName() (string, error) {
	hostname, err := RunCommandOnHost("cat", "/etc/hostname")
	if err != nil {
		return "", fmt.Errorf("Fail to get host name: %+v", err)
	}

	return strings.TrimSuffix(string(hostname), "\n"), nil
}

// GetAPIServerFQDN gets the API Server FQDN from the kubeconfig file
func GetAPIServerFQDN() (string, error) {
	output, err := RunCommandOnHost("cat", "/var/lib/kubelet/kubeconfig")

	if err != nil {
		return "", fmt.Errorf("Can't open kubeconfig file: %+v", err)
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		index := strings.Index(line, "server: ")
		if index >= 0 {
			fqdn := line[index+len("server: "):]
			fqdn = strings.Replace(fqdn, "https://", "", -1)
			fqdn = strings.Replace(fqdn, ":443", "", -1)
			return fqdn, nil
		}
	}

	return "", errors.New("Could not find server definitions in kubeconfig")
}

// RunCommandOnHost runs a command on host system
func RunCommandOnHost(command string, arg ...string) (string, error) {
	args := []string{"--target", "1", "--mount", "--uts", "--ipc", "--net", "--pid"}
	args = append(args, "--")
	args = append(args, command)
	args = append(args, arg...)

	cmd := exec.Command("nsenter", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Fail to run command on host: %+v", err)
	}

	return string(out), nil
}

// RunCommandOnContainerWithOutputStreams runs a command on container system and returns both the stdout and stderr output streams
func RunCommandOnContainerWithOutputStreams(command string, arg ...string) (CommandOutputStreams, error) {
	cmd := exec.Command(command, arg...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	outputStreams := CommandOutputStreams{stdout.String(), stderr.String()}

	if err != nil {
		return outputStreams, fmt.Errorf("Fail to run command in container: %s", fmt.Sprint(err)+": "+stderr.String())
	}

	return outputStreams, nil
}

// RunCommandOnContainer  runs a command on container system and returns the stdout output stream
func RunCommandOnContainer(command string, arg ...string) (string, error) {
	outputStreams, err := RunCommandOnContainerWithOutputStreams(command, arg...)
	return outputStreams.Stdout, err
}

// RunBackgroundCommand starts running a command on a container system in the background and returns its process ID
func RunBackgroundCommand(command string, arg ...string) (int, error) {
	cmd := exec.Command(command, arg...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Start()
	if err != nil {
		return 0, fmt.Errorf("Fail to start background command in container: %s", fmt.Sprint(err)+": "+stderr.String())
	}
	return cmd.Process.Pid, nil
}

// Finds and kills a process with a given process ID
func KillProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("Failed to find process with pid %d to kill: ", pid, fmt.Sprint(err))
	}
	if err := process.Kill(); err != nil {
		return err
	}
	return nil
}

// Tries to issue an HTTP GET request up to maxRetries times
func GetUrlWithRetries(url string, maxRetries int) ([]byte, error) {
	var resp *http.Response
	var err error
	for i := 1; i <= maxRetries; i++ {
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		log.Printf("Error curling %s: %+v. %d retries remaining...\n", url, err, maxRetries-i)
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

// WriteToFile writes data to a file
func WriteToFile(fileName string, data string) error {
	f, err := os.Create(fileName)
	defer f.Close()
	if err != nil {
		return fmt.Errorf("Fail to create file %s: %+v", fileName, err)
	}

	_, err = f.Write([]byte(data))
	if err != nil {
		return fmt.Errorf("Fail to write data to file %s: %+v", fileName, err)
	}

	return nil
}

// CreateCollectorDir creates a working dir for a collector
func CreateCollectorDir(name string) (string, error) {
	hostName, err := GetHostName()
	if err != nil {
		return "", err
	}

	creationTimeStamp, err := GetCreationTimeStamp()
	if err != nil {
		return "", err
	}

	rootPath := filepath.Join("/aks-periscope", strings.Replace(creationTimeStamp, ":", "-", -1), hostName, "collector", name)
	err = os.MkdirAll(rootPath, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("Fail to create dir %s: %+v", rootPath, err)
	}

	return rootPath, nil
}

// CreateDiagnosticDir creates a working dir for diagnostic
func CreateDiagnosticDir() (string, error) {
	hostName, err := GetHostName()
	if err != nil {
		return "", err
	}

	creationTimeStamp, err := GetCreationTimeStamp()
	if err != nil {
		return "", err
	}

	rootPath := filepath.Join("/aks-periscope", strings.Replace(creationTimeStamp, ":", "-", -1), hostName, "diagnoser")
	err = os.MkdirAll(rootPath, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("Fail to create dir %s: %+v", rootPath, err)
	}

	return rootPath, nil
}

// CreateKubeConfigFromServiceAccount creates kubeconfig based on creds in service account
func CreateKubeConfigFromServiceAccount() error {
	token, err := RunCommandOnContainer("cat", "/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return err
	}

	_, err = RunCommandOnContainer("kubectl", "config", "set-credentials", "aks-periscope-service-account", "--token="+token)
	if err != nil {
		return err
	}

	_, err = RunCommandOnContainer("kubectl", "config", "set-cluster", "aks-periscope-cluster", "--server=https://kubernetes.default.svc.cluster.local:443", "--certificate-authority=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return err
	}

	_, err = RunCommandOnContainer("kubectl", "config", "set-context", "aks-periscope-context", "--user=aks-periscope-service-account", "--cluster=aks-periscope-cluster")
	if err != nil {
		return err
	}

	_, err = RunCommandOnContainer("kubectl", "config", "use-context", "aks-periscope-context")
	if err != nil {
		return err
	}

	return nil
}

// GetCreationTimeStamp returns a create timestamp
func GetCreationTimeStamp() (string, error) {
	creationTimeStamp, err := RunCommandOnContainer("kubectl", "get", "pods", "--all-namespaces", "-l", "app=aks-periscope", "-o", "jsonpath=\"{.items[0].metadata.creationTimestamp}\"")
	if err != nil {
		return "", err
	}

	return creationTimeStamp[1 : len(creationTimeStamp)-1], nil
}

// WriteToCRD writes diagnostic data to CRD
func WriteToCRD(fileName string, key string) error {
	hostName, err := GetHostName()
	if err != nil {
		return err
	}

	crdName := "aks-periscope-diagnostic" + "-" + hostName

	jsonBytes, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}

	patchContent := fmt.Sprintf("{\"spec\":{%q:%q}}", key, string(jsonBytes))

	_, err = RunCommandOnContainer("kubectl", "-n", "aks-periscope", "patch", "apd", crdName, "-p", patchContent, "--type=merge")
	if err != nil {
		return err
	}

	return nil
}

// CreateCRD creates a CRD object
func CreateCRD() error {
	hostName, err := GetHostName()
	if err != nil {
		return err
	}

	crdName := "aks-periscope-diagnostic" + "-" + hostName

	writeDiagnosticCRD(crdName)

	_, err = RunCommandOnContainer("kubectl", "apply", "-f", "aks-periscope-diagnostic-crd.yaml")
	if err != nil {
		return err
	}

	return nil
}

// GetResourceList gets a list of all resources of given type in a specified namespace
func GetResourceList(kubeCmds []string, separator string) ([]string, error) {
	outputStreams, err := RunCommandOnContainerWithOutputStreams("kubectl", kubeCmds...)

	if err != nil {
		return nil, err
	}

	resourceList := outputStreams.Stdout
	// If the resource is not found within the cluster, then log a message and do not return any resources.
	if len(resourceList) == 0 {
		err := fmt.Errorf("No '%s' resource found in the cluster for given kubectl command", kubeCmds[1])
		return nil, err
	}

	return strings.Split(strings.Trim(resourceList, "\""), separator), nil
}

func writeDiagnosticCRD(crdName string) error {
	f, err := os.Create("aks-periscope-diagnostic-crd.yaml")
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString("apiVersion: \"aks-periscope.azure.github.com/v1\"\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("kind: Diagnostic\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("metadata:\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("  name: " + crdName + "\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("  namespace: aks-periscope\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("spec:\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("  networkconfig: \"\"\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("  networkoutbound: \"\"\n")
	if err != nil {
		return err
	}

	return nil
}
