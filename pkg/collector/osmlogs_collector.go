package collector

import (
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
	meshList, err := utils.GetResourceList([]string{"get", "deployments", "--all-namespaces", "-l", "app=osm-controller", "-o", "jsonpath={..meshName}"}, " ")
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
		"allResourcesTable":               []string{"get", "all", "--all-namespaces", "-l", "app.kubernetes.io/name=openservicemesh.io", "-o", "wide"},
		"allResourcesConfigs":             []string{"get", "all", "--all-namespaces", "-l", "app.kubernetes.io/name=openservicemesh.io", "-o", "json"},
		"mutatingWebhookConfigurations":   []string{"get", "MutatingWebhookConfiguration", "--all-namespaces", "-l", "app.kubernetes.io/name=openservicemesh.io", "-o", "json"},
		"validatingWebhookConfigurations": []string{"get", "ValidatingWebhookConfiguration", "--all-namespaces", "-l", "app.kubernetes.io/name=openservicemesh.io", "-o", "json"},
	}

	for fileName, kubeCmds := range groundTruthMap {
		if err = collector.CollectKubectlOutputToCollectorFiles(rootPath, fileName, kubeCmds); err != nil {
			log.Printf("Failed to collect %s for OSM: %+v", fileName, err)
		}
	}

	meshNamespacesList, err := utils.GetResourceList([]string{"get", "deployments", "--all-namespaces", "-l", "app=osm-controller", "-o", "jsonpath={..metadata.namespace}"}, " ")
	if err != nil {
		return err
	}

	for _, meshName := range meshList {
		namespacesInMesh, err := utils.GetResourceList([]string{"get", "namespaces", "--all-namespaces", "-l", "openservicemesh.io/monitored-by=" + meshName, "-o", "jsonpath={..name}"}, " ")
		if err != nil {
			log.Printf("Failed to get namespaces within osm mesh '%s': %+v\n", meshName, err)
			continue
		}
		osmNamespaces := append(namespacesInMesh, meshNamespacesList...)
		if err = collectDataFromNamespaces(collector, osmNamespaces, rootPath, meshName); err != nil {
			log.Printf("Failed to collect data from OSM monitored namespaces: %+v", err)
		}
		if err = collectControllerLogs(collector, rootPath, meshName); err != nil {
			log.Printf("Failed to collect OSM controller logs for mesh %s: %+v", meshName, err)
		}
	}

	return nil
}

// ** collectDataFromNamespaces collects data for namespaces monitored by a given mesh **
func collectDataFromNamespaces(collector *OsmLogsCollector, namespaces []string, rootPath, meshName string) error {
	for _, namespace := range namespaces {
		var namespaceResourcesMap = map[string][]string{
			meshName + "_" + namespace + "_metadata":                 []string{"get", "namespaces", namespace, "-o", "jsonpath={..metadata}", "-o", "json"},
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
			meshName + "_" + namespace + "_pods_list":                []string{"get", "pods", "-n", namespace},
		}

		if err := collectPodConfigs(collector, rootPath, meshName, namespace); err != nil {
			log.Printf("Failed to collect pod logs for ns %s", namespace, err)
		}

		for fileName, kubeCmds := range namespaceResourcesMap {
			if err := collector.CollectKubectlOutputToCollectorFiles(rootPath, fileName, kubeCmds); err != nil {
				log.Printf("Failed to collect %s in OSM monitored namespace %s: %+v", fileName, namespace, err)
			}
		}
	}

	return nil
}

// **collectPodConfigs collects data for pods in a given mesh **
func collectPodConfigs(collector *OsmLogsCollector, rootPath, meshName, namespace string) error {
	pods, err := utils.GetResourceList([]string{"get", "pods", "-n", namespace, "-o", "jsonpath={..metadata.name}"}, " ")
	if err != nil {
		return err
	}
	for _, podName := range pods {
		kubeCmds := []string{"get", "pods", "-n", namespace, podName, "-o", "json"}
		if err := collector.CollectKubectlOutputToCollectorFiles(rootPath, meshName+"_"+namespace+"_pod_config_"+podName, kubeCmds); err != nil {
			log.Printf("Failed to collect config for pod %s in OSM monitored namespace %s: %+v", podName, namespace, err)
		}
	}
	return nil
}

// **collectControllerLogs collects logs of every OSM controller in a given mesh **
func collectControllerLogs(collector *OsmLogsCollector, rootPath, meshName string) error {
	controllerInfos, err := utils.GetResourceList([]string{"get", "pods", "--all-namespaces", "-l", "app=osm-controller", "-o", "custom-columns=NAME:{..metadata.name},NAMESPACE:{..metadata.namespace}"}, "\n")
	if err != nil {
		return err
	}
	for _, controllerInfo := range controllerInfos[1:] {
		controllerInfoParts := strings.Fields(controllerInfo)
		if len(controllerInfoParts) > 0 {
			podName := controllerInfoParts[0]
			namespace := controllerInfoParts[1]
			if err := collector.CollectKubectlOutputToCollectorFiles(rootPath, meshName+"_controller_logs_"+podName, []string{"logs", "-n", namespace, podName}); err != nil {
				return err
			}
		}
	}
	return nil
}
