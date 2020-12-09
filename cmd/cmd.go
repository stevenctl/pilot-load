package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/grpclog"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/howardjohn/pilot-load/pkg/simulation"
	"github.com/howardjohn/pilot-load/pkg/simulation/model"

	"istio.io/pkg/log"
)

var (
	pilotAddress      = "localhost:15010"
	kubeconfig        = os.Getenv("KUBECONFIG")
	configFile        = ""
	loggingOptions    = defaultLogOptions()
	adscConfig        = model.AdscConfig{}
	nodeMetadata      = []string{}
	impersonateConfig = model.ImpersonateConfig{
		Replicas: 1,
		Selector: string(model.SidecarSelector),
	}
	proberConfig = model.ProberConfig{
		Replicas: 1,
	}
	qps = 100
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&pilotAddress, "pilot-address", "p", pilotAddress, "address to pilot")
	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "k", kubeconfig, "kubeconfig")
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", configFile, "config file")
	rootCmd.PersistentFlags().IntVar(&qps, "qps", qps, "qps for kube client")

	rootCmd.PersistentFlags().IntVar(&adscConfig.Count, "adsc.count", adscConfig.Count, "number of adsc connections to make")
	rootCmd.PersistentFlags().StringSliceVar(&nodeMetadata, "meta", []string{}, "node metadata values in the pair KEY=value. No need to prepend ISTIO_META.")

	rootCmd.PersistentFlags().DurationVar(&impersonateConfig.Delay, "impersonate.delay", impersonateConfig.Delay, "delay between each connection")
	rootCmd.PersistentFlags().IntVar(&impersonateConfig.Replicas, "impersonate.replicas", impersonateConfig.Replicas, "number of connections to make for each pod")
	rootCmd.PersistentFlags().StringVar(&impersonateConfig.Selector, "impersonate.selector", impersonateConfig.Selector, "selector to use {sidecar,external,both}")

	rootCmd.PersistentFlags().DurationVar(&proberConfig.Delay, "prober.delay", proberConfig.Delay, "delay between each virtual service")
	rootCmd.PersistentFlags().IntVar(&proberConfig.DelayThreshold, "prober.delay-threshold", proberConfig.DelayThreshold, "if set, there will be no delay until we have this many virtual services")
	rootCmd.PersistentFlags().IntVar(&proberConfig.Replicas, "prober.replicas", proberConfig.Replicas, "number of virtual services to make")
	rootCmd.PersistentFlags().StringVar(&proberConfig.GatewayAddress, "prober.address", proberConfig.GatewayAddress, "address to gateway")
}

func defaultLogOptions() *log.Options {
	o := log.DefaultOptions()

	// These scopes are, at the default "INFO" level, too chatty for command line use
	o.SetOutputLevel("dump", log.WarnLevel)

	return o
}

var rootCmd = &cobra.Command{
	Use:          "pilot-load",
	Short:        "open XDS connections to pilot",
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return log.Configure(loggingOptions)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		grpclog.SetLoggerV2(grpclog.NewLoggerV2(ioutil.Discard, ioutil.Discard, ioutil.Discard))
		sim := ""
		if len(args) > 0 {
			sim = args[0]
		}
		if kubeconfig == "" {
			kubeconfig = filepath.Join(os.Getenv("HOME"), "/.kube/config")
		}

		if qps == 0 {
			qps = 100
		}
		a := model.Args{
			PilotAddress:      pilotAddress,
			KubeConfig:        kubeconfig,
			Qps:               qps,
			AdsConfig:         adscConfig,
			ImpersonateConfig: impersonateConfig,
			ProberConfig:      proberConfig,
		}

		switch sim {
		case "cluster":
			config, err := readClusterConfigFile(configFile)
			if err != nil {
				return fmt.Errorf("failed to read config file: %v", err)
			}
			a.ClusterConfig = config.ApplyDefaults()
			applyMetaOverrides(a.ClusterConfig.NodeMetadata)
			logConfig(a.ClusterConfig)
			logClusterConfig(a.ClusterConfig)
			return simulation.Cluster(a)
		case "adsc":
			if config, err := readAdscConfigFile(configFile); err == nil {
				a.AdsConfig = config
			} else {
				return fmt.Errorf("failed to read config file %v", err)
			}
			if a.AdsConfig.NodeMetadata == nil {
				a.AdsConfig.NodeMetadata = map[string]interface{}{}
			}
			applyMetaOverrides(a.AdsConfig.NodeMetadata)
			logConfig(a.AdsConfig)
			return simulation.Adsc(a)
		case "impersonate":
			logConfig(a.ImpersonateConfig)
			return simulation.Impersonate(a)
		case "prober":
			logConfig(a.ProberConfig)
			return simulation.GatewayProber(a)
		case "api":
			return simulation.ApiServer(a)
		default:
			return fmt.Errorf("unknown simulation %v. Expected: {cluster, adsc, impersonate, prober}", sim)
		}
	},
}

func applyMetaOverrides(meta map[string]interface{}) {
	for _, arg := range nodeMetadata {
		p := strings.Split(arg, "=")
		if len(p) != 2 {
			log.Infof("meta arg %s is in the incorrect format", arg)
		}
		meta[p[0]] = p[1]
	}
}

func logConfig(config interface{}) {
	bytes, err := yaml.Marshal(config)
	if err != nil {
		panic(err.Error())
	}
	log.Infof("Starting simulation with config:\n%v", string(bytes))
}

func logClusterConfig(config model.ClusterConfig) {
	namespaces, pods, applications := 0, 0, 0
	for _, ns := range config.Namespaces {
		namespaces += ns.Replicas
		for _, app := range ns.Applications {
			applications += app.Replicas * ns.Replicas
			pods += app.Replicas * app.Instances * ns.Replicas
		}
	}
	log.Infof("Initial configuration: %d namespaces, %d applications, and %d pods", namespaces, applications, pods)
}

var defaultConfig = model.ClusterConfig{
	Namespaces: []model.NamespaceConfig{{
		Applications: []model.ApplicationConfig{{Instances: 1}},
	}},
}

func readClusterConfigFile(filename string) (model.ClusterConfig, error) {
	if filename == "" {
		return defaultConfig, nil
	}
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return model.ClusterConfig{}, fmt.Errorf("failed to read configFile file: %v", filename)
	}
	config := model.ClusterConfig{}
	if err := yaml.Unmarshal(bytes, &config); err != nil {
		return config, fmt.Errorf("failed to unmarshall configFile: %v", err)
	}
	return config, err
}

func readAdscConfigFile(filename string) (model.AdscConfig, error) {
	if filename == "" {
		return model.AdscConfig{}, nil
	}
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return model.AdscConfig{}, fmt.Errorf("failed to read configFile file: %v", filename)
	}
	config := model.AdscConfig{}
	if err := yaml.Unmarshal(bytes, &config); err != nil {
		return model.AdscConfig{}, fmt.Errorf("failed to unmarshall configFile: %v", err)
	}
	return config, err
}

func Execute() {
	loggingOptions.AttachCobraFlags(rootCmd)
	hiddenFlags := []string{"log_as_json", "log_rotate", "log_rotate_max_age", "log_rotate_max_backups",
		"log_rotate_max_size", "log_stacktrace_level", "log_target", "log_caller"}
	for _, opt := range hiddenFlags {
		_ = rootCmd.PersistentFlags().MarkHidden(opt)
	}
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
