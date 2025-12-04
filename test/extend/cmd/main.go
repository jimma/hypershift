package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	cmd "github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	e "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	et "github.com/openshift-eng/openshift-tests-extension/pkg/extension/extensiontests"
	g "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	// Import the extend test package to register Ginkgo specs
	- "github.com/openshift/hypershift/test/extend"
)

const (
	extensionName = "hypershift"
)

func main() {
	// Create registry and extension
	registry := e.NewRegistry()
	extension := e.NewExtension("openshift", "payload", extensionName)

	// Build test specs from Ginkgo suite
	specs, err := g.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building test specs: %v\n", err)
		os.Exit(1)
	}
	// Debug: print number of specs discovered
	fmt.Fprintf(os.Stderr, "DEBUG: Discovered %d test specs\n", len(specs))

	// Label tests based on their names and attributes using Walk
	// Walk allows us to inspect each spec and add labels without filtering
	specs = specs.Walk(func(spec *et.ExtensionTestSpec) {
		// Platform labels
		if et.NameContains("AWS")(spec) {
			spec.Labels.Insert("aws")
		}
		if et.NameContains("ROSA")(spec) {
			spec.Labels.Insert("rosa")
		}
		if et.NameContains("ARO")(spec) {
			spec.Labels.Insert("aro")
		}
		if et.NameContains("Azure")(spec) {
			spec.Labels.Insert("azure")
		}
		if et.NameContains("HyperShiftAKSINSTALL")(spec) {
			spec.Labels.Insert("aks")
		}
		// Feature labels
		if et.NameContains("ema-HyperShiftMGMT-Critical")(spec) {
			spec.Labels.Insert("jimma")
		}
		if et.NameContains("autoscal")(spec) || et.NameContains("Autoscal")(spec) {
			spec.Labels.Insert("autoscaling")
		}
		if et.NameContains("HostedCluster")(spec) || et.NameContains("hosted cluster")(spec) {
			spec.Labels.Insert("hosted-cluster")
		}
		if et.NameContains("nodepool")(spec) || et.NameContains("NodePool")(spec) {
			spec.Labels.Insert("nodepool")
		}
		if et.NameContains("etcd")(spec) {
			spec.Labels.Insert("etcd")
		}
		if et.NameContains("network")(spec) || et.NameContains("ingress")(spec) {
			spec.Labels.Insert("networking")
		}

		// Attribute labels
		if et.NameContains("Critical")(spec) {
			spec.Labels.Insert("critical")
		}
		if et.NameContains("High")(spec) {
			spec.Labels.Insert("high")
		}
		if et.NameContains("Longduration")(spec) {
			spec.Labels.Insert("longduration")
		}
		if et.NameContains("Serial")(spec) {
			spec.Labels.Insert("serial")
		}
		if et.NameContains("Disruptive")(spec) {
			spec.Labels.Insert("disruptive")
		}
		if et.NameContains("Flaky")(spec) {
			spec.Labels.Insert("flaky")
		}
		if et.NameContains("NonPreRelease")(spec) {
			spec.Labels.Insert("non-pre-release")
		}
	})

	// Add the suite prefix label to all specs
	specs = specs.AddLabel(fmt.Sprintf("%s/all", extensionName))

	// Debug: print number of specs after labeling
	fmt.Fprintf(os.Stderr, "DEBUG: After labeling: %d test specs\n", len(specs))

	// Define test suites with qualifiers based on test naming patterns
	// Core suite - all HyperShift QE tests
	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/all", extensionName),
	})

	// Platform-specific suites
	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/aws", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="aws")`,
		},
	})
	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/azure", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="azure") || labels.exists(l, l=="aro")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/rosa", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="rosa")`,
		},
	})

	// Feature-based suites
	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/jimma", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="jimma")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/autoscaling", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="autoscaling")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/hosted-cluster", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="hosted-cluster")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/nodepool", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="nodepool")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/etcd", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="etcd")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/networking", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="networking")`,
		},
	})

	// Attribute-based suites
	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/critical", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="critical")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/longduration", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="longduration")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/serial", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="serial")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/disruptive", extensionName),
		Qualifiers: []string{
			`labels.exists(l, l=="disruptive")`,
		},
	})

	// Add all labeled specs to the extension
	extension.AddSpecs(specs)

	// Debug: verify specs were added
	fmt.Fprintf(os.Stderr, "DEBUG: Added specs to extension\n")

	// Register extension
	registry.Register(extension)
	root := &cobra.Command{
		Use:   "hypershift-extend-test",
		Short: "HyperShift extend test runner",
		Long: `A test runner for HyperShift extend tests using the openshift-tests-extension framework.

This tool allows to discover, list, and run HyperShift extend tests in a standardized way.

Examples:
  # List all available test suites
  hypershift-extend-test list suites

  # List all tests
  hypershift-extend-test list tests

  # Run suite
  hypershift-extend-test run-suite hypershift/smoke

  # Run test
  hypershift-extend-test run-test "run-test "[sig-hypershift] Hypershift openshift-test-extension smoke test"`,
	}

	root.AddCommand(cmd.DefaultExtensionCommands(registry)...)

	// Execute the command
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
