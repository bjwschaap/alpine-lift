package lift

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
)

type Lift struct {
	DataURL string
	Data    *AlpineData
}

// New returns a new Lift instance with initial configuration
func New(dataURL string) (*Lift, error) {
	return &Lift{
		DataURL: dataURL,
		Data:    InitAlpineData(),
	}, nil
}

// This is the main program loop
func (l *Lift) Start() error {
	log.Info("Lift starting...")
	// If url not provided, read it from the kernel boot parameters
	if l.DataURL == "" {
		var err error
		if l.DataURL, err = getDataURL(); err != nil {
			return err
		}
	}
	log.WithField("url", l.DataURL).Info("downloading alpine-data file")
	data, err := downloadFile(l.DataURL)
	if err != nil {
		return err
	}

	if err = yaml.Unmarshal(data, l.Data); err != nil {
		return err
	}

	log.Info("Executing alpine-setup")
	if err = l.alpineSetup(); err != nil {
		return err
	}

	log.Info("Setup APK and Packages")
	if err = l.setupAPK(); err != nil {
		return err
	}

	log.Info("Setting SSHD configuration")
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

	if l.Data.DRP.InstallRunner {
		log.Info("Installing dr-provision runner")
		if err = l.drpSetup(); err != nil {
			return err
		}
	}

	log.Info("Writing files")
	for _, wf := range l.Data.WriteFiles {
		perm, err := strconv.ParseUint(wf.Permissions, 8, 32)
		if err != nil {
			return fmt.Errorf("Error reading permissions: %s", err)
		}
		log.Infof("Creating %s", wf.Path)
		err = os.MkdirAll(filepath.Dir(wf.Path), 0711)
		if err != nil {
			return fmt.Errorf("Error creating %s: %s", filepath.Dir(wf.Path), err)
		}
		err = ioutil.WriteFile(wf.Path, []byte(wf.Content), os.FileMode(perm))
		if err != nil {
			log.Debugf("error writing file: %s", err)
		}
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
func getDataURL() (string, error) {
	cmdline, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}
	for _, a := range strings.Fields(string(cmdline)) {
		if strings.HasPrefix(a, "alpine-data=") {
			return strings.Split(a, "=")[1], nil
		}
	}
	return "", errors.New("Could not find alpine-data kernel boot parameter")
}
