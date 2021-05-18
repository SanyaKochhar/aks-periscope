package collector

import (
	"fmt"
	"log"
	"strings"

	"github.com/Azure/aks-periscope/pkg/interfaces"
	"github.com/Azure/aks-periscope/pkg/utils"
)

// SmiLogsCollector defines an SmiLogs Collector struct
type SmiLogsCollector struct {
	BaseCollector
}

var _ interfaces.Collector = &SmiLogsCollector{}

// NewSmiLogsCollector is a constructor
func NewSmiLogsCollector(exporter interfaces.Exporter) *SmiLogsCollector {
	return &SmiLogsCollector{
		BaseCollector: BaseCollector{
			collectorType: SmiLogs,
			exporter:      exporter,
		},
	}
}

// Collect implements the interface method
func (collector *SmiLogsCollector) Collect() error {
	smiCrdList, err := getSmiCustomResourceDefinitions()
	if err != nil {
		return err
	}

	rootPath, err := utils.CreateCollectorDir(collector.GetName())
	if err != nil {
		return err
	}

	for _, smiCrd := range smiCrdList {
		var smiResourcesMap = map[string][]string{
			smiCrd + "_list.tsv":     []string{"get", smiCrd, "--all-namespaces", "-o=wide"},
			smiCrd + "_configs.json": []string{"get", smiCrd, "--all-namespaces", "-o=json"},
		}

		for fileName, kubeCmd := range smiResourcesMap {
			if err = collector.CollectKubectlOutputToCollectorFiles(rootPath, fileName, kubeCmd); err != nil {
				log.Printf("Failed to collect %s for SMI: %+v", fileName, err)
			}
		}
	}

	return nil
}

// getSmiCustomResourceDefinitions returns SMI CRDs within the cluster
func getSmiCustomResourceDefinitions() ([]string, error) {
	outputStreams, err := utils.RunCommandOnContainerWithOutputStreams("kubectl", "get", "CustomResourceDefinitions", "--all-namespaces", "-o=jsonpath={..metadata.name}")
	if err != nil {
		return nil, err
	}

	crdList := strings.Split(strings.Trim(outputStreams.Stdout, "\""), " ")
	// If the CRD is not found within the cluster, then log a message and do not return any resources.
	if len(crdList) == 0 {
		err := fmt.Errorf("No CustomResourceDefinitions resource found in the cluster.")
		return nil, err
	}

	var smiCrdList []string
	for _, crd := range crdList {
		if strings.Contains(crd, "smi-spec.io") {
			smiCrdList = append(smiCrdList, crd)
		}
	}

	return smiCrdList, nil
}
