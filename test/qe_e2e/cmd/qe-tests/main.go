package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	cmd "github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	e "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	et "github.com/openshift-eng/openshift-tests-extension/pkg/extension/extensiontests"
	g "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	// Import the qe_e2e test package to register Ginkgo specs
	_ "github.com/openshift/hypershift/test/qe_e2e"
)

const (
	extensionName = "hypershift-qe"
	suitePrefix   = "hypershift-qe"
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
		if et.NameContains("HyperShiftMGMT")(spec) {
			spec.Labels.Insert("hypershift-mgmt")
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
	specs = specs.AddLabel(fmt.Sprintf("%s/all", suitePrefix))

	// Debug: print number of specs after labeling
	fmt.Fprintf(os.Stderr, "DEBUG: After labeling: %d test specs\n", len(specs))

	// Define test suites with qualifiers based on test naming patterns
	// Core suite - all HyperShift QE tests
	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/all", suitePrefix),
	})

	// Platform-specific suites
	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/aws", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="aws")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/azure", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="azure") || labels.exists(l, l=="aro")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/rosa", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="rosa")`,
		},
	})

	// Feature-based suites
	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/management", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="hypershift-mgmt")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/autoscaling", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="autoscaling")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/hosted-cluster", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="hosted-cluster")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/nodepool", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="nodepool")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/etcd", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="etcd")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/networking", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="networking")`,
		},
	})

	// Attribute-based suites
	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/critical", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="critical")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/longduration", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="longduration")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/serial", suitePrefix),
		Qualifiers: []string{
			`labels.exists(l, l=="serial")`,
		},
	})

	extension.AddSuite(e.Suite{
		Name: fmt.Sprintf("%s/disruptive", suitePrefix),
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

	// Create root command
	root := &cobra.Command{
		Use:   "qe-tests",
		Short: "HyperShift QE E2E test runner",
		Long: `A test runner for HyperShift QE E2E tests using the openshift-tests-extension framework.

This tool allows you to discover, list, and run HyperShift QE tests in a standardized way.

Available test suites are organized by:
  - Platform: aws, azure, rosa, aks
  - Feature: management, autoscaling, hosted-cluster, nodepool, etcd, networking
  - Attributes: critical, longduration, serial, disruptive

Examples:
  # List all available test suites
  qe-tests list-suites

  # List all tests
  qe-tests list-tests

  # Run all critical tests
  qe-tests run hypershift-qe/critical

  # Run AWS-specific tests
  qe-tests run hypershift-qe/aws

  # Run nodepool tests
  qe-tests run hypershift-qe/nodepool`,
	}

	// Add default extension commands (list-suites, list-tests, run, etc.)
	root.AddCommand(cmd.DefaultExtensionCommands(registry)...)

	// Execute the command
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
