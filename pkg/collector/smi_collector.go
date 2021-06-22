package collector

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/Azure/aks-periscope/pkg/interfaces"
	"github.com/Azure/aks-periscope/pkg/utils"
)

// SmiCollector defines an Smi Collector struct
type SmiCollector struct {
	BaseCollector
}

var _ interfaces.Collector = &SmiCollector{}

// NewSmiCollector is a constructor
func NewSmiCollector(exporter interfaces.Exporter) *SmiCollector {
	return &SmiCollector{
		BaseCollector: BaseCollector{
			collectorType: Smi,
			exporter:      exporter,
		},
	}
}

// Collect implements the interface method
func (collector *SmiCollector) Collect() error {
	// Get all CustomResourceDefinitions in the cluster
	allCrdsList, err := utils.GetResourceList([]string{"get", "crds", "-o", "jsonpath={..metadata.name}"}, " ")
	if err != nil {
		return err
	}

	// Directory where logs will be written to
	rootPath, err := utils.CreateCollectorDir(collector.GetName())
	if err != nil {
		return err
	}

	// Filter to obtain a list of Smi CustomResourceDefinitions in the cluster
	crdNameContainsSmiPredicate := func(s string) bool { return strings.Contains(s, "smi-spec.io") }
	smiCrdsList := filter(allCrdsList, crdNameContainsSmiPredicate)
	if len(smiCrdsList) == 0 {
		// TODO should this return an error?
		log.Printf("Cluster does not contain any SMI CustomResourceDefinitions\n")
		return nil
	}

	collectSmiCrdDefinitions(collector, filepath.Join(rootPath, "smi_crd_definitions"), smiCrdsList)
	collectSmiCustomResourcesFromAllNamespaces(collector, filepath.Join(rootPath, "smi_custom_resources"), smiCrdsList)

	return nil
}

func collectSmiCrdDefinitions(collector *SmiCollector, rootPath string, smiCrdsList []string) {
	for _, smiCrd := range smiCrdsList {
		fileName := smiCrd + "_definition.yaml"
		kubeCmd := []string{"get", "crd", smiCrd, "-o", "yaml"}
		if err := collector.CollectKubectlOutputToCollectorFiles(rootPath, fileName, kubeCmd); err != nil {
			log.Printf("Failed to collect %s: %+v", fileName, err)
		}
	}
}

func collectSmiCustomResourcesFromAllNamespaces(collector *SmiCollector, rootPath string, smiCrdsList []string) {
	// Get all namespaces in the cluster
	namespacesList, err := utils.GetResourceList([]string{"get", "namespaces", "-o", "jsonpath={..metadata.name}"}, " ")
	if err != nil {
		log.Printf("Failed to list namespaces in the cluster: %+v", err)
		return
	}

	for _, namespace := range namespacesList {
		namespaceRootPath := filepath.Join(rootPath, "namespace_"+namespace)
		collectSmiCustomResourcesFromSingleNamespace(collector, namespaceRootPath, smiCrdsList, namespace)
	}
}

func collectSmiCustomResourcesFromSingleNamespace(collector *SmiCollector, rootPath string, smiCrdsList []string, namespace string) {
	for _, smiCrdType := range smiCrdsList {
		// get all custom resources of this smi crd type
		customResourcesList, err := utils.GetResourceList([]string{"get", smiCrdType, "-n", namespace, "-o", "jsonpath={..metadata.name}"}, " ")
		if err != nil {
			log.Printf("Failed to list custom resources of type %s in namespace %s: %+v", smiCrdType, namespace, err)
			continue
		}

		customResourcesRootPath := filepath.Join(rootPath, smiCrdType+"_custom_resources")
		for _, customResourceName := range customResourcesList {
			fileName := smiCrdType + "_" + customResourceName + ".yaml"
			kubeCmd := []string{"get", smiCrdType, customResourceName, "-n", namespace, "-o", "yaml"}
			if err := collector.CollectKubectlOutputToCollectorFiles(customResourcesRootPath, fileName, kubeCmd); err != nil {
				log.Printf("Failed to collect %s: %+v", fileName, err)
			}
		}
	}
}

func filter(ss []string, test func(string) bool) (ret []string) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}
	return
}
