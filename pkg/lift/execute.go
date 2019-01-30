package lift

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	log "github.com/sirupsen/logrus"
)

const (
	drpcliBin    = "/usr/local/bin/drpcli"
	drpcliRCFile = "/etc/init.d/drpcli"
)

// executes the setup-alpine script, using a generated answerfile
func (l *Lift) alpineSetup() error {
	var cmd *exec.Cmd
	var input string
	f, err := generateFileFromTemplate(*answerFile, l.Data)
	if err != nil {
		return err
	}
	if l.Data.RootPasswd == "" {
		cmd = exec.Command("setup-alpine", "-e", "-f", f)
	} else {
		cmd = exec.Command("setup-alpine", "-f", f)
		// setup-alpine script asks for root password on stdin
		input = fmt.Sprintf("%s\n%s\n", l.Data.RootPasswd, l.Data.RootPasswd)

	}
	// If not silenced, show setup-alpine output on stdout
	if !silent {
		cmd.Stdout = os.Stdout
	}
	env := append(os.Environ(), "VARFS=xfs")
	env = append(env, "SWAP_SIZE=4096")
	cmd.Env = env

	if l.Data.ScratchDisk != "" {
		// answer 'y' to confirm disk overwrite
		input += "y\n"
	}
	cmd.Stdin = bytes.NewBuffer([]byte(input))
	// Ignore any errors, since exit code can be 1 if
	// e.g. service is already running.
	_ = cmd.Run()
	// Remove answerfile
	//_ = os.Remove(f)
	return nil
}

// parses sshd_config, writes authorized_keys file and restarts sshd service
func (l *Lift) sshdSetup() error {
	if err := parseConfigFile("/etc/ssh/sshd_config", " ", l.getSSHDKVMap()); err != nil {
		return err
	}
	if err := l.addSSHKeys(); err != nil {
		return err
	}
	if err := doService("sshd", RESTART); err != nil {
		return err
	}
	return nil
}

// opens or creates authorized_keys file, and adds ssh keys
// from alpine-data
func (l *Lift) addSSHKeys() error {
	if l.Data.SSHDConfig.AuthorizedKeys != nil && len(l.Data.SSHDConfig.AuthorizedKeys) > 0 {
		file, err := openOrCreate("/root/.ssh/authorized_keys")
		if err != nil {
			return err
		}
		defer file.Close()
		for _, key := range l.Data.SSHDConfig.AuthorizedKeys {
			if _, err = file.WriteString(fmt.Sprintf("%s\n", key)); err != nil {
				return err
			}
		}
	}
	return nil
}

// downloads drpcli and installs it as a service
func (l *Lift) drpSetup() error {
	// First download drpcli
	if _, err := os.Stat(drpcliBin); os.IsNotExist(err) {
		url := fmt.Sprintf("%s/drpcli.amd64.linux", l.Data.DRP.AssetsURL)
		log.WithField("url", url).Debug("Downloading drpcli")
		drpcli, err := downloadFile(url)
		if err != nil {
			return err
		}
		log.Debugf("Saving drpcli to %s", drpcliBin)
		err = ioutil.WriteFile(drpcliBin, drpcli, 0755)
		if err != nil {
			return err
		}
	}

	// then check RC file
	if _, err := os.Stat(drpcliRCFile); os.IsNotExist(err) {
		log.Debug("Generating drpcli rc service file")
		rcfile, err := generateFileFromTemplate(*drpcliInit, l.Data)
		if err != nil {
			return err
		}
		log.Debugf("Copying service file to %s", drpcliRCFile)
		cmd := exec.Command("mv", rcfile, drpcliRCFile)
		err = cmd.Run()
		if err != nil {
			return err
		}
		log.Debug("Setting execute permission")
		cmd = exec.Command("chmod", "+x", drpcliRCFile)
		err = cmd.Run()
		if err != nil {
			return err
		}
		log.Debug("Add drpcli service to default runlevel")
		cmd = exec.Command("rc-update", "add", "drpcli")
		err = cmd.Run()
		if err != nil {
			return err
		}
	}

	log.Info("Starting dr-provision runner")
	_ = doService("drpcli", START)
	return nil
}

func (l *Lift) setupAPK() error {
	rfile, err := generateFileFromTemplate(*repoFile, l.Data.Packages.Repositories)
	if err != nil {
		return err
	}
	log.Debug("Setting up repositories")
	cmd := exec.Command("mv", rfile, "/etc/apk/repositories")
	err = cmd.Run()
	if err != nil {
		return err
	}
	if l.Data.Packages.Update {
		log.Debug("Executing apk update")
		cmd := exec.Command("apk", "update")
		err = cmd.Run()
		if err != nil {
			return err
		}
	}
	if l.Data.Packages.Upgrade {
		log.Debug("Executing apk upgrade")
		cmd := exec.Command("apk", "upgrade")
		err = cmd.Run()
		if err != nil {
			return err
		}
	}
	for _, p := range l.Data.Packages.Uninstall {
		log.WithField("package", p).Debug("Executing apk del")
		cmd := exec.Command("apk", "del", p)
		err = cmd.Run()
		if err != nil {
			return err
		}
	}
	for _, p := range l.Data.Packages.Install {
		log.WithField("package", p).Debug("Executing apk add")
		cmd := exec.Command("apk", "add", p)
		err = cmd.Run()
		if err != nil {
			return err
		}
	}
	return nil
}

func (l *Lift) setMOTD() error {
	if l.Data.MOTD != "" {
		err := os.Truncate("/etc/motd", 0)
		if err != nil {
			return err
		}
		file, err := os.OpenFile("/etc/motd", os.O_RDWR|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err = file.WriteString(fmt.Sprintf("%s\n", l.Data.MOTD)); err != nil {
			return err
		}
	}
	return nil
}

func (l *Lift) createFiles() error {
	for _, wf := range l.Data.WriteFiles {
		var data []byte

		perm, err := strconv.ParseUint(wf.Permissions, 8, 32)
		if err != nil {
			return fmt.Errorf("Error reading permissions: %s", err)
		}
		log.Infof("Creating %s", wf.Path)
		err = os.MkdirAll(filepath.Dir(wf.Path), 0711)
		if err != nil {
			return fmt.Errorf("Error creating %s: %s", filepath.Dir(wf.Path), err)
		}
		if wf.Content != "" {
			data = []byte(wf.Content)

		} else if wf.ContentURL != "" {
			if data, err = downloadFile(wf.ContentURL); err != nil {
				return err
			}
		}
		err = ioutil.WriteFile(wf.Path, data, os.FileMode(perm))
		if err != nil {
			log.Debugf("error writing file: %s", err)
		}
		if wf.Owner != "" {
			cmd := exec.Command("chown", wf.Owner, wf.Path)
			err = cmd.Run()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
