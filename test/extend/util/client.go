package util

import (
	"bytes"
	"fmt"
	g "github.com/onsi/ginkgo/v2"
	configv1 "github.com/openshift/api/config/v1"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kubernetes/test/e2e/framework"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sigs.k8s.io/yaml"
	"strings"
)

type CLI struct {
	execPath        string
	verb            string
	configPath      string
	adminConfigPath string
	guestConfigPath string // Added for OTP compatibility

	// directory with static manifests, each file is expected to be a single manifest
	// manifest files can be stored under directory tree
	staticConfigManifestDir string

	token              string
	username           string
	globalArgs         []string
	commandArgs        []string
	finalArgs          []string
	namespacesToDelete []string
	stdin              *bytes.Buffer
	stdout             io.Writer
	stderr             io.Writer

	// env allows setting environment variables for the command (when nil, the environment
	// is inherited from the current process)
	env []string
	// addEnvVars allows adding environment variables on top of what is defined in env.
	addEnvVars map[string]string

	verbose              bool
	showInfo             bool // control framework.Logf output
	withoutNamespace     bool
	withoutKubeconf      bool
	asGuestKubeconf      bool
	withManagedNamespace bool
	kubeFramework        *framework.Framework

	// read from a static manifest directory (set through STATIC_CONFIG_MANIFEST_DIR env)
	configObjects     []runtime.Object
	resourcesToDelete []resourceRef
}
type resourceRef struct {
	Resource  schema.GroupVersionResource
	Namespace string
	Name      string
}

type staticObject struct {
	APIVersion, Kind, Namespace, Name string
}

func NewCLIForMonitorTest(project string) *CLI {
	cli := &CLI{
		kubeFramework: &framework.Framework{
			SkipNamespaceCreation: true,
			BaseName:              project,
			Options: framework.Options{
				ClientQPS:   20,
				ClientBurst: 50,
			},
			Timeouts: framework.NewTimeoutContext(),
		},
		username:                "admin",
		execPath:                "oc",
		adminConfigPath:         KubeConfigPath(),
		staticConfigManifestDir: StaticConfigManifestDir(),
		showInfo:                true,
		withoutNamespace:        true,
	}

	// Called only once (assumed the objects will never get modified)
	cli.setupStaticConfigsFromManifests()
	return cli
}

func (c *CLI) setupStaticConfigsFromManifests() {
	if len(c.staticConfigManifestDir) > 0 {
		err, objects := collectConfigManifestsFromDir(c.staticConfigManifestDir)
		if err != nil {
			panic(err)
		}
		c.configObjects = objects
	}
}
func collectConfigManifestsFromDir(configManifestsDir string) (error, []runtime.Object) {
	objects := []runtime.Object{}
	knownObjects := make(map[staticObject]string)

	err := filepath.Walk(configManifestsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		object := &metav1.TypeMeta{}
		err = yaml.Unmarshal(body, &object)
		if err != nil {
			return err
		}

		if object.APIVersion == "config.openshift.io/v1" {
			switch object.Kind {
			case "Infrastructure":
				config := &configv1.Infrastructure{}
				err = yaml.Unmarshal(body, &config)
				if err != nil {
					return err
				}
				key := staticObject{APIVersion: object.APIVersion, Kind: object.Kind, Namespace: config.Namespace, Name: config.Name}
				if objPath, exists := knownObjects[key]; exists {
					return fmt.Errorf("object %v duplicated under %v", path, objPath)
				}
				objects = append(objects, config)
				knownObjects[key] = path
			case "Network":
				config := &configv1.Network{}
				err = yaml.Unmarshal(body, &config)
				if err != nil {
					return err
				}
				key := staticObject{APIVersion: object.APIVersion, Kind: object.Kind, Namespace: config.Namespace, Name: config.Name}
				if objPath, exists := knownObjects[key]; exists {
					return fmt.Errorf("object %v duplicated under %v", path, objPath)
				}
				objects = append(objects, config)
				knownObjects[key] = path
			default:
				return fmt.Errorf("unknown 'config.openshift.io/v1' kind: %v", object.Kind)
			}
		} else {
			return fmt.Errorf("unknown apiversion: %v", object.APIVersion)
		}

		return nil
	})

	return err, objects
}

func KubeConfigPath() string {
	// can't use gomega in this method since it is used outside of It()
	return os.Getenv("KUBECONFIG")
}
func StaticConfigManifestDir() string {
	return os.Getenv("STATIC_CONFIG_MANIFEST_DIR")
}

// SetGuestKubeconf instructs the guest cluster kubeconf file is set
func (c *CLI) SetGuestKubeconf(guestKubeconf string) *CLI {
	c.guestConfigPath = guestKubeconf
	return c
}

// AsGuestKubeconf instructs the command should take kubeconfig of guest cluster
func (c CLI) AsGuestKubeconf() *CLI {
	c.asGuestKubeconf = true
	c.withoutNamespace = true // if you want to use guest cluster config to opeate guest cluster, you have to set
	//withoutNamespace as true (like calling WithoutNamespace), so you can not get ns of
	// management cluster, and you have to set ns of guest cluster in Args.
	return &c
}

func (c *CLI) AsAdmin() *CLI {
	nc := *c
	nc.configPath = c.adminConfigPath
	return &nc
}

func (c CLI) WithoutNamespace() *CLI {
	c.withoutNamespace = true
	return &c
}

// KubeFramework returns Kubernetes framework which contains helper functions
// specific for Kubernetes resources
func (c *CLI) KubeFramework() *framework.Framework {
	return c.kubeFramework
}

// Namespace returns the name of the namespace used in the current test case.
// If the namespace is not set, an empty string is returned.
func (c *CLI) Namespace() string {
	if c.kubeFramework.Namespace == nil {
		return ""
	}
	return c.kubeFramework.Namespace.Name
}

// setOutput allows to override the default command output
func (c *CLI) setOutput(out io.Writer) *CLI {
	c.stdout = out
	return c
}

// Args sets the additional arguments for the OpenShift CLI command
func (c *CLI) Args(args ...string) *CLI {
	c.commandArgs = args
	return c
}

// Output executes the command and returns stdout/stderr combined into one string
func (c *CLI) Output() (string, error) {
	var buff bytes.Buffer
	_, _, err := c.outputs(&buff, &buff)
	return strings.TrimSpace(string(buff.Bytes())), err
}

// Outputs executes the command and returns the stdout/stderr output as separate strings
func (c *CLI) Outputs() (string, string, error) {
	var stdOutBuff, stdErrBuff bytes.Buffer
	return c.outputs(&stdOutBuff, &stdErrBuff)
}

func (c *CLI) outputs(stdOutBuff, stdErrBuff *bytes.Buffer) (string, string, error) {
	cmd, err := c.start(stdOutBuff, stdErrBuff)
	if err != nil {
		return "", "", err
	}
	err = cmd.Wait()

	stdOutBytes := stdOutBuff.Bytes()
	stdErrBytes := stdErrBuff.Bytes()
	stdOut := strings.TrimSpace(string(stdOutBytes))
	stdErr := strings.TrimSpace(string(stdErrBytes))

	switch err.(type) {
	case nil:
		c.stdout = bytes.NewBuffer(stdOutBytes)
		c.stderr = bytes.NewBuffer(stdErrBytes)
		return stdOut, stdErr, nil
	case *exec.ExitError:
		framework.Logf("Error running %s %s:\nStdOut>\n%s\nStdErr>\n%s\n", c.execPath, RedactBearerToken(strings.Join(c.finalArgs, " ")), stdOut, stdErr)
		wrappedErr := fmt.Errorf("Error running %s %s:\nStdOut>\n%s\nStdErr>\n%s\n%w\n", c.execPath, RedactBearerToken(strings.Join(c.finalArgs, " ")), stdOut[getStartingIndexForLastN(stdOutBytes, 4096):], stdErr[getStartingIndexForLastN(stdErrBytes, 4096):], err)
		return stdOut, stdErr, wrappedErr
	default:
		FatalErr(fmt.Errorf("unable to execute %q: %v", c.execPath, err))
		// unreachable code
		return "", "", nil
	}
}

// getStartingIndexForLastN calculates a byte offset in a byte slice such that when using
// that offset, we get the last N (size) bytes.
func getStartingIndexForLastN(byteString []byte, size int) int {
	len := len(byteString)
	if len < size {
		// byte slice is less than size, so use all of it.
		return 0
	}
	return len - size
}

// FatalErr exits the test in case a fatal error has occurred.
func FatalErr(msg interface{}) {
	// the path that leads to this being called isn't always clear...
	fmt.Fprintln(g.GinkgoWriter, string(debug.Stack()))
	framework.Failf("%v", msg)
}

func (c *CLI) printCmd() string {
	return strings.Join(c.finalArgs, " ")
}

func (c *CLI) start(stdOutBuff, stdErrBuff *bytes.Buffer) (*exec.Cmd, error) {
	c.finalArgs = append(c.globalArgs, c.commandArgs...)
	if c.verbose {
		fmt.Printf("DEBUG: oc %s\n", c.printCmd())
	}
	cmd := exec.Command(c.execPath, c.finalArgs...)
	cmd.Stdin = c.stdin
	// Redact any bearer token information from the log.
	// Only log if showInfo is enabled (controlled by util_otp)
	if c.showInfo {
		framework.Logf("Running '%s %s'", c.execPath, RedactBearerToken(strings.Join(c.finalArgs, " ")))
	}

	cmd.Env = c.env
	if len(c.addEnvVars) > 0 {
		// This is a nil check to allow setting empty environment with Env()
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		for name, value := range c.addEnvVars {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", name, value))
		}
	}

	cmd.Stdout = stdOutBuff
	cmd.Stderr = stdErrBuff
	err := cmd.Start()

	return cmd, err
}

var reToken = regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[^\s"]+|((BearerToken:\s*")[^"]+)`)

func RedactBearerToken(args string) string {
	return reToken.ReplaceAllString(args, `${1}${3}<redacted>`)
}

// Run executes given OpenShift CLI command verb (iow. "oc <verb>").
// This function also override the default 'stdout' to redirect all output
// to a buffer and prepare the global flags such as namespace and config path.
func (c *CLI) Run(commands ...string) *CLI {
	in, out, errout := &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	nc := &CLI{
		execPath:        c.execPath,
		verb:            commands[0],
		kubeFramework:   c.KubeFramework(),
		adminConfigPath: c.adminConfigPath,
		configPath:      c.configPath,
		username:        c.username,
		globalArgs:      commands,
	}
	if len(c.configPath) > 0 {
		nc.globalArgs = append([]string{fmt.Sprintf("--kubeconfig=%s", c.configPath)}, nc.globalArgs...)
	}
	if len(c.configPath) == 0 && len(c.token) > 0 {
		nc.globalArgs = append([]string{fmt.Sprintf("--token=%s", c.token)}, nc.globalArgs...)
	}
	if !c.withoutNamespace {
		nc.globalArgs = append([]string{fmt.Sprintf("--namespace=%s", c.Namespace())}, nc.globalArgs...)
	}
	nc.stdin, nc.stdout, nc.stderr = in, out, errout
	return nc.setOutput(c.stdout)
}
