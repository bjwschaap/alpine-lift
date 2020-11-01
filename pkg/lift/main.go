package lift

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
)

// Lift contains all configuration
type Lift struct {
	DataURL        string
	RequestHeaders http.Header
	Data           *AlpineData
}

// New returns a new Lift instance with initial configuration
func New(dataURL string, requestHeaders http.Header) (*Lift, error) {
	return &Lift{
		DataURL:        dataURL,
		RequestHeaders: requestHeaders,
		Data:           InitAlpineData(),
	}, nil
}

// Start contains the main program loop
func (l *Lift) Start() error {
	// If alpine-lift-silent kernel boot param is set, silence all logging/output
	if s, err := getKernelBootParam("alpine-lift-silent"); err == nil && s != "" {
		log.SetOutput(ioutil.Discard)
		silent = true
	}

	// If alpine-lift-debug-log kernel boot param is set, enable debug logging/output
	if s, err := getKernelBootParam("alpine-lift-debug-log"); err == nil && s != "" {
		log.SetLevel(log.DebugLevel)
	}

	log.Info("Lift starting...")
	// If url not provided, read it from the kernel boot parameters
	if l.DataURL == "" {
		var err error
		if l.DataURL, err = getKernelBootParam("alpine-data"); err != nil {
			return err
		}
		if l.DataURL == "" {
			return errors.New("alpine-data URL not set")
		}
	}
	log.WithField("url", l.DataURL).Info("downloading alpine-data file")
	data, err := downloadFile(l.DataURL, l.RequestHeaders)
	if err != nil {
		return err
	}

	if err = yaml.Unmarshal(data, l.Data); err != nil {
		return err
	}

	log.Info("Set root password")
	if err = l.rootPasswdSetup(); err != nil {
		return err
	}

	log.Info("Executing setup-disk")
	if err = l.scratchDiskSetup(); err != nil {
		return err
	}

	log.Info("Add additional disks")
	if err = l.diskSetup(); err != nil {
		return err
	}

	log.Info("Setting Hostname")
	if err = l.setHostname(); err != nil {
		return err
	}

	log.Info("Setup Network Interfaces")
	if err = l.networkSetup(); err != nil {
		return err
	}

	log.Info("Setup DNS")
	if err = l.dnsSetup(); err != nil {
		return err
	}

	log.Info("Setup Up Network Proxy")
	if err = l.proxySetup(); err != nil {
		return err
	}

	log.Info("Setup APK and Packages")
	if err = l.setupAPK(); err != nil {
		return err
	}

	log.Info("Setup SSHD configuration")
	if err = l.sshdSetup(); err != nil {
		return err
	}

	log.Info("Creating groups")
	for _, grp := range l.Data.Groups {
		cmd := exec.Command("addgroup", grp)
		log.Infof("Creating group %s", grp)
		if err = cmd.Run(); err != nil {
			log.Debugf("Error creating group %s: %v", grp, err)
		}
	}

	log.Info("Creating Users")
	for _, user := range l.Data.Users {
		log.Infof("Creating user %s", user.Name)
		if err = createOSUser(user); err != nil {
			log.Debugf("Error creating user %s: %v", user.Name, err)
		}
	}

	log.Info("Setup NTP")
	if err = l.ntpSetup(); err != nil {
		return err
	}

	if l.Data.DRP.InstallRunner {
		log.Info("Installing dr-provision runner")
		if err = l.drpSetup(); err != nil {
			return err
		}
	}

	log.Info("Setup MTA")
	if err = l.mtaSetup(); err != nil {
		return err
	}

	log.Info("Writing files")
	if err = l.createFiles(); err != nil {
		return err
	}

	log.Info("Setting MOTD")
	if err = l.setMOTD(); err != nil {
		return err
	}

	log.Info("Executing post-install commands")
	for _, c := range l.Data.RunCMD {
		c = append([]string{"-c"}, c...)
		cmd := exec.Command("sh", c...)
		cmd.Env = os.Environ()
		log.Debugf("exec: sh -c \"%s\"", c[1:])
		err := cmd.Run()
		if err != nil {
			log.Debugf("err: %s", err)
		}
	}

	// Final SSH restart because of added keys etc.
	_ = doService("sshd", RESTART)

	// Delete the lift binary from the system
	if l.Data.UnLift {
		log.Info("Removing lift binary from the system")
		binPath, err := os.Readlink("/proc/self/exe")
		if err != nil {
			return err
		}
		log.WithField("path", binPath).Debug("os.Remove")
		if err = os.Remove(binPath); err != nil {
			return err
		}
	}

	log.Info("Lift successfully completed")
	return nil
}

// tries to get the alpine-data parameter from the kernel parameters in /proc/cmdline
func getKernelBootParam(key string) (string, error) {
	cmdline, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}
	for _, a := range strings.Fields(string(cmdline)) {
		if strings.HasPrefix(a, fmt.Sprintf("%s=", key)) {
			return strings.Split(a, "=")[1], nil
		}
	}
	return "", nil
}
