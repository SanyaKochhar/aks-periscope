package collector

import (
	"fmt"
	"log"
	"strings"

	"github.com/Azure/aks-periscope/pkg/interfaces"
	"github.com/Azure/aks-periscope/pkg/utils"
)

// OsmLogsCollector defines an OsmLogs Collector struct
type OsmLogsCollector struct {
	BaseCollector
}

var _ interfaces.Collector = &OsmLogsCollector{}

// NewOsmLogsCollector is a constructor
func NewOsmLogsCollector(exporter interfaces.Exporter) *OsmLogsCollector {
	return &OsmLogsCollector{
		BaseCollector: BaseCollector{
			collectorType: OsmLogs,
			exporter:      exporter,
		},
	}
}

// Collect implements the interface method
func (collector *OsmLogsCollector) Collect() error {
	// Get all OSM deployments in order to collect information for various resources across all meshes in the cluster
	meshList, err := getResourceList("deployments", "app=osm-controller", "-o=jsonpath={..meshName}", " ")
	if err != nil {
		return err
	}

	// Directory where OSM logs will be written to
	rootPath, err := utils.CreateCollectorDir(collector.GetName())
	if err != nil {
		return err
	}

	// ** Collect ground truth on OSM resources **
	var groundTruthMap = map[string][]string{
		"allResourcesTable":               []string{"get", "all", "--all-namespaces", "--selector=app.kubernetes.io/name=openservicemesh.io", "-o=wide"},
		"allResourcesConfigs":             []string{"get", "all", "--all-namespaces", "--selector=app.kubernetes.io/name=openservicemesh.io", "-o=json"},
		"mutatingWebhookConfigurations":   []string{"get", "MutatingWebhookConfiguration", "--all-namespaces", "--selector=app.kubernetes.io/name=openservicemesh.io", "-o=json"},
		"validatingWebhookConfigurations": []string{"get", "ValidatingWebhookConfiguration", "--all-namespaces", "--selector=app.kubernetes.io/name=openservicemesh.io", "-o=json"},
	}

	for fileName, kubeCmd := range groundTruthMap {
		if err = collector.collectKubeResourceToFile(rootPath, fileName, kubeCmd); err != nil {
			fmt.Printf("Failed to collect %s for OSM: %+v", fileName, err)
		}
	}

	meshNamespacesList, err := getResourceList("deployments", "app=osm-controller", "-o=jsonpath={..metadata.namespace}", " ")
	if err != nil {
		return err
	}

	for _, meshName := range meshList {
		namespacesInMesh, err := getResourceList("namespaces", "openservicemesh.io/monitored-by="+meshName, "-o=jsonpath={..name}", " ")
		if err != nil {
			log.Printf("Failed to get namespaces within osm mesh '%s': %+v\n", meshName, err)
			continue
		}
		osmNamespaces := append(namespacesInMesh, meshNamespacesList...)
		if err = collectDataFromNamespaces(collector, osmNamespaces, rootPath, meshName); err != nil {
			fmt.Printf("Failed to collect data from OSM monitored namespaces: %+v", err)
		}
		if err = collectControllerLogs(collector, rootPath, meshName); err != nil {
			fmt.Printf("Failed to collect OSM controller logs for mesh %s: %+v", meshName, err)
		}
	}

	return nil
}

// ** Collect data for namespaces monitored by a given mesh **
func collectDataFromNamespaces(collector *OsmLogsCollector, namespaces []string, rootPath, meshName string) error {
	for _, namespace := range namespaces {
		var namespaceResourcesMap = map[string][]string{
			meshName + "_" + namespace + "_metadata":                 []string{"get", "namespaces", namespace, "-o=jsonpath={..metadata}", "-o=json"},
			meshName + "_" + namespace + "_services_table":           []string{"get", "services", "-n", namespace},
			meshName + "_" + namespace + "_services_configs":         []string{"get", "services", "-n", namespace, "-o", "json"},
			meshName + "_" + namespace + "_endpoints_table":          []string{"get", "endpoints", "-n", namespace},
			meshName + "_" + namespace + "_endpoints_configs":        []string{"get", "endpoints", "-n", namespace, "-o", "json"},
			meshName + "_" + namespace + "_configmaps_table":         []string{"get", "configmaps", "-n", namespace},
			meshName + "_" + namespace + "_configmaps_configs":       []string{"get", "configmaps", "-n", namespace, "-o", "json"},
			meshName + "_" + namespace + "_ingresses_table":          []string{"get", "ingresses", "-n", namespace},
			meshName + "_" + namespace + "_ingresses_configs":        []string{"get", "ingresses", "-n", namespace, "-o", "json"},
			meshName + "_" + namespace + "_service_accounts_table":   []string{"get", "serviceaccounts", "-n", namespace},
			meshName + "_" + namespace + "_service_accounts_configs": []string{"get", "serviceaccounts", "-n", namespace, "-o", "json"},
		}

		for fileName, kubeCmd := range namespaceResourcesMap {
			if err := collector.collectKubeResourceToFile(rootPath, fileName, kubeCmd); err != nil {
				fmt.Printf("Failed to collect %s in OSM monitored namespace %s: %+v", fileName, namespace, err)
			}
		}
	}

	return nil
}

// ** Collect logs of every OSM controller in a given mesh **
func collectControllerLogs(collector *OsmLogsCollector, rootPath, meshName string) error {
	controllerInfos, err := getResourceList("pods", "app=osm-controller", "-o=custom-columns=NAME:{..metadata.name},NAMESPACE:{..metadata.namespace}", "\n")
	if err != nil {
		return err
	}
	for _, controllerInfo := range controllerInfos[1:] {
		controllerInfoParts := strings.Fields(controllerInfo)
		if len(controllerInfoParts) > 0 {
			podName := controllerInfoParts[0]
			namespace := controllerInfoParts[1]
			if err := collector.collectKubeResourceToFile(rootPath, meshName+"_controller_logs_"+podName, []string{"logs", "-n", namespace, podName}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Helper function to get all resoures of given type in the cluster
func getResourceList(resource, label, outputFormat, separator string) ([]string, error) {
	outputStreams, err := utils.RunCommandOnContainerWithOutputStreams("kubectl", "get", resource, "--all-namespaces", "--selector", label, outputFormat)
	if err != nil {
		return nil, err
	}

	resourceList := outputStreams.Stdout
	// If the resource is not found within the cluster, then log a message and do not return any resources.
	if len(resourceList) == 0 {
		err := fmt.Errorf("No '%s' resource with the label '%s' found in the cluster.", resource, label)
		return nil, err
	}

	return strings.Split(strings.Trim(resourceList, "\""), separator), nil
}
