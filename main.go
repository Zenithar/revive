package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/fatih/color"
	"github.com/mgechev/dots"
	"github.com/mgechev/revive/config"
	"github.com/mgechev/revive/lint"
	"github.com/mgechev/revive/logging"
	"github.com/mitchellh/go-homedir"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

func fail(err string) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func main() {
	log, err := logging.GetLogger()
	if err != nil {
		fail(err.Error())
	}

	conf, err := config.GetConfig(configPath)
	if err != nil {
		fail(err.Error())
	}
	formatter, err := config.GetFormatter(formatterName)
	if err != nil {
		fail(err.Error())
	}
	if setExitStatus {
		conf.ErrorCode = 1
		conf.WarningCode = 1
	}

	if len(excludePaths) == 0 { // if no excludes were set in the command line
		excludePaths = conf.Exclude // use those from the configuration
	}

	packages, err := getPackages(excludePaths)
	if err != nil {
		fail(err.Error())
	}

	revive := lint.New(func(file string) ([]byte, error) {
		return ioutil.ReadFile(file)
	})

	lintingRules, err := config.GetLintingRules(conf)
	if err != nil {
		fail(err.Error())
	}

	log.Println("Config loaded")

	failures, err := revive.Lint(packages, lintingRules, *conf)
	if err != nil {
		fail(err.Error())
	}

	formatChan := make(chan lint.Failure)
	exitChan := make(chan bool)

	var output string
	go (func() {
		output, err = formatter.Format(formatChan, *conf)
		if err != nil {
			fail(err.Error())
		}
		exitChan <- true
	})()

	exitCode := 0
	for f := range failures {
		if f.Confidence < conf.Confidence {
			continue
		}
		if exitCode == 0 {
			exitCode = conf.WarningCode
		}
		if c, ok := conf.Rules[f.RuleName]; ok && c.Severity == lint.SeverityError {
			exitCode = conf.ErrorCode
		}
		if c, ok := conf.Directives[f.RuleName]; ok && c.Severity == lint.SeverityError {
			exitCode = conf.ErrorCode
		}

		formatChan <- f
	}

	close(formatChan)
	<-exitChan
	if output != "" {
		fmt.Println(output)
	}

	os.Exit(exitCode)
}

func normalizeSplit(strs []string) []string {
	res := []string{}
	for _, s := range strs {
		t := strings.Trim(s, " \t")
		if len(t) > 0 {
			res = append(res, t)
		}
	}
	return res
}

func getPackages(excludePaths arrayFlags) ([][]string, error) {
	globs := normalizeSplit(flag.Args())
	if len(globs) == 0 {
		globs = append(globs, ".")
	}

	packages, err := dots.ResolvePackages(globs, normalizeSplit(excludePaths))
	if err != nil {
		return nil, err
	}

	return packages, nil
}

type arrayFlags []string

func (i *arrayFlags) String() string {
	return strings.Join([]string(*i), " ")
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

var (
	configPath    string
	excludePaths  arrayFlags
	formatterName string
	help          bool
	versionFlag   bool
	setExitStatus bool
)

var originalUsage = flag.Usage

func getLogo() string {
	return color.YellowString(` _ __ _____   _(_)__  _____
| '__/ _ \ \ / / \ \ / / _ \
| | |  __/\ V /| |\ V /  __/
|_|  \___| \_/ |_| \_/ \___|`)
}

func getCall() string {
	return color.MagentaString("revive -config c.toml -formatter friendly -exclude a.go -exclude b.go ./...")
}

func getBanner() string {
	return fmt.Sprintf(`
%s

Example:
  %s
`, getLogo(), getCall())
}

func buildDefaultConfigPath() string {
	var result string
	if homeDir, err := homedir.Dir(); err == nil {
		result = filepath.Join(homeDir, "revive.toml")
		if _, err := os.Stat(result); err != nil {
			result = ""
		}
	}

	return result
}

func init() {
	// Force colorizing for no TTY environments
	if os.Getenv("REVIVE_FORCE_COLOR") == "1" {
		color.NoColor = false
	}

	flag.Usage = func() {
		fmt.Println(getBanner())
		originalUsage()
	}

	// command line help strings
	const (
		configUsage     = "path to the configuration TOML file, defaults to $HOME/revive.toml, if present (i.e. -config myconf.toml)"
		excludeUsage    = "list of globs which specify files to be excluded (i.e. -exclude foo/...)"
		formatterUsage  = "formatter to be used for the output (i.e. -formatter stylish)"
		versionUsage    = "get revive version"
		exitStatusUsage = "set exit status to 1 if any issues are found, overwrites errorCode and warningCode in config"
	)

	defaultConfigPath := buildDefaultConfigPath()

	flag.StringVar(&configPath, "config", defaultConfigPath, configUsage)
	flag.Var(&excludePaths, "exclude", excludeUsage)
	flag.StringVar(&formatterName, "formatter", "", formatterUsage)
	flag.BoolVar(&versionFlag, "version", false, versionUsage)
	flag.BoolVar(&setExitStatus, "set_exit_status", false, exitStatusUsage)
	flag.Parse()

	// Output build info (version, commit, date and builtBy)
	if versionFlag {
		var buildInfo string
		if date != "unknown" && builtBy != "unknown" {
			buildInfo = fmt.Sprintf("Built\t\t%s by %s\n", date, builtBy)
		}

		if commit != "none" {
			buildInfo = fmt.Sprintf("Commit:\t\t%s\n%s", commit, buildInfo)
		}

		if version == "dev" {
			bi, ok := debug.ReadBuildInfo()
			if ok {
				version = bi.Main.Version
				if strings.HasPrefix(version, "v") {
					version = bi.Main.Version[1:]
				}
				if len(buildInfo) == 0 {
					fmt.Printf("version %s\n", version)
					os.Exit(0)
				}
			}
		}

		fmt.Printf("Version:\t%s\n%s", version, buildInfo)
		os.Exit(0)
	}
}
