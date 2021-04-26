package collector

import (
	"path/filepath"
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
	rootPath, err := utils.CreateCollectorDir(collector.GetName())
	if err != nil {
		return err
	}
	// can define resources to query in deployment.yaml and iterate through the commands+resources needed and create multiple files
	// see kubeobject collector for example

	// * Get osm resources as table
	allResourcesFile := filepath.Join(rootPath, "allResources")
	output, err := utils.RunCommandOnContainer("kubectl", "get", "all", "--all-namespaces", "--selector", "app.kubernetes.io/name=openservicemesh.io")
	if err != nil {
		return err
	}

	err = utils.WriteToFile(allResourcesFile, output)
	if err != nil {
		return err
	}

	collector.AddToCollectorFiles(allResourcesFile)

	// * Get osm resource configs
	allResourceConfigsFile := filepath.Join(rootPath, "allResourceConfigs")
	output, err = utils.RunCommandOnContainer("kubectl", "get", "all", "--all-namespaces", "--selector", "app.kubernetes.io/name=openservicemesh.io", "-o", "json")
	if err != nil {
		return err
	}

	err = utils.WriteToFile(allResourceConfigsFile, output)
	if err != nil {
		return err
	}

	collector.AddToCollectorFiles(allResourceConfigsFile)

	// * Collect information for various resources across all meshes in the cluster
	meshList, err := getResourceList("deployments", "app=osm-controller", "-o=jsonpath={..meshName}", " ")
	if err != nil {
		return err
	}
	resources := []string{"configmap", "serviceaccount", "service", "endpoint", "ingress"}
	for _, meshName := range meshList {
		namespacesInMesh, err := getResourceList("namespaces", "openservicemesh.io/monitored-by="+meshName, "-o=jsonpath={..name}", " ")
		if err != nil {
			return err
		}

		collectControllerLogs(collector, rootPath, meshName)
		if err != nil {
			return err
		}

		collectNamespaceMetadata(collector, namespacesInMesh, rootPath, meshName)
		if err != nil {
			return err
		}

		for _, resource := range resources {
			err = collectResource(collector, resource, rootPath, meshName)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// * Collects metadata for each ns in a given mesh
func collectNamespaceMetadata(collector *OsmLogsCollector, namespaces []string, rootPath, meshName string) error {
	for _, namespace := range namespaces {
		namespaceMetadataFile := filepath.Join(rootPath, meshName+"_"+namespace+"_"+"metadata")
		namespaceMetadata, err := utils.RunCommandOnContainer("kubectl", "get", "namespaces", namespace, "-o=jsonpath={..metadata}", "-o", "json")
		if err != nil {
			return err
		}
		err = utils.WriteToFile(namespaceMetadataFile, namespaceMetadata)
		if err != nil {
			return err
		}
		collector.AddToCollectorFiles(namespaceMetadataFile)
	}
	return nil
}

// Collects specified OSM resource
func collectResource(collector *OsmLogsCollector, resource, rootPath, meshName string) error {
	resourceNameList, err := getResourceList(resource, "app.kubernetes.io/name=openservicemesh.io", "-o=jsonpath={.items[*].metadata.name}", " ")
	if err != nil {
		return err
	}
	resourceNsList, err := getResourceList(resource, "app.kubernetes.io/name=openservicemesh.io", "-o=jsonpath={.items[*].metadata.namespace}", " ")
	if err != nil {
		return err
	}

	for i, resourceName := range resourceNameList {
		resourcePathFile := filepath.Join(rootPath, meshName+"_"+resourceNsList[i]+"_"+resource+"_"+resourceName)
		resourceDetail, err := utils.RunCommandOnContainer("kubectl", "get", resource, resourceName, "-n", resourceNsList[i], "-o", "json")
		if err != nil {
			return err
		}
		err = utils.WriteToFile(resourcePathFile, resourceDetail)
		if err != nil {
			return err
		}
		collector.AddToCollectorFiles(resourcePathFile)
	}
	return nil
}

// ** Collects logs of every OSM controller in a given mesh **
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

			logsFile := filepath.Join(rootPath, meshName+"_controller_logs_"+podName)
			logs, err := utils.RunCommandOnContainer("kubectl", "logs", "-n", namespace, podName)
			if err != nil {
				return err
			}
			err = utils.WriteToFile(logsFile, logs)
			if err != nil {
				return err
			}
			collector.AddToCollectorFiles(logsFile)
		}

	}
	return nil
}

// Helper function to get all resoures of given type in the cluster
func getResourceList(resource, label, outputFormat, separator string) ([]string, error) {
	resourceList, err := utils.RunCommandOnContainer("kubectl", "get", resource, "--all-namespaces", "--selector", label, outputFormat)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.Trim(resourceList, "\""), separator), nil
}
