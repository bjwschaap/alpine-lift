package lift

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

const (
	START   = "start"
	RESTART = "restart"
	STOP    = "stop"
	RELOAD  = "reload"
	ZAP     = "zap"
)

// rewrites a config file with values from alpine-data
func parseConfigFile(path, sep string, kv map[string]string) error {
	conf, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	out := findReplace(conf, sep, kv)
	err = ioutil.WriteFile(path, out, 0644)
	if err != nil {
		return err
	}
	return nil
}

// Terribly inefficient way to find keys and replace them with
// our own value... Refactor at a later point in time...
// sep defines the separator (typically " ", ":" or "=")
// This function uses exact (case-sensitive) match on purpose.
func findReplace(conf []byte, sep string, kv map[string]string) []byte {
	lines := strings.Split(string(conf), "\n")
	for k, v := range kv {
		found := false
		for i, l := range lines {
			if !strings.HasPrefix(l, "#") && strings.Contains(l, k) {
				lines[i] = fmt.Sprintf("%s%s%s", k, sep, v)
				found = true
			}
		}
		if !found {
			lines = append(lines, fmt.Sprintf("%s%s%s", k, sep, v))
		}
	}
	out := strings.Join(lines, "\n")
	return []byte(out)
}

// this function takes a path to a file, and tries to
// open it, creating it if it doesn't exist.
// Don't forget to close the file!!
func openOrCreate(path string) (*os.File, error) {
	var err error
	file := new(os.File)

	// MkDirAll is safe/idempotent
	err = os.MkdirAll(filepath.Dir(path), 0700)
	if err != nil {
		return file, err
	}

	// try and create the file (prevents race conditions vs checking existence first)
	file, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			err = nil
			// create failed because it exists; open existing file
			file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				return file, err
			}
		} else {
			return file, err
		}
	}
	return file, nil
}

// interact with openrc to start, stop, restart or reload a service
func doService(name string, action string) error {
	cmd := exec.Command("service", name, action)
	err := cmd.Run()
	return err
}

// Creates an OS user
func createOSUser(u User) error {
	args := []string{u.Name}
	var input []byte

	if u.NoCreateHomeDir {
		args = append([]string{"-H"}, args...)
	} else if u.HomeDir != "" {
		args = append([]string{"-h", u.HomeDir}, args...)
	}
	if u.Description != "" {
		args = append([]string{"-g", u.Description}, args...)
	}
	if u.PrimaryGroup != "" {
		args = append([]string{"-G", u.PrimaryGroup}, args...)
	}
	if u.System {
		args = append([]string{"-S"}, args...)
	}
	if u.Password != "" {
		input = []byte(fmt.Sprintf("%s\n%s\n", u.Password, u.Password))
	} else {
		args = append([]string{"-D"}, args...)
	}
	if u.Shell != "" {
		args = append([]string{"-s", u.Shell}, args...)
	}

	cmd := exec.Command("adduser", args...)
	if len(input) > 0 {
		cmd.Stdin = bytes.NewBuffer(input)
	}
	err := cmd.Run()
	if err != nil {
		log.Debugf("Error creating user %s: %s", u.Name, err)
	}

	if u.Groups != nil && len(u.Groups) > 0 {
		for _, g := range u.Groups {
			cmd := exec.Command("adduser", u.Name, g)
			err = cmd.Run()
			if err != nil {
				log.Debugf("Error adding %s to %s: %s", u.Name, g, err)
			}
		}
	}

	if u.SSHAuthorizedKeys != nil && len(u.SSHAuthorizedKeys) > 0 {
		cmd := exec.Command("grep", u.Name, "/etc/passwd")
		var b bytes.Buffer
		cmd.Stdout = &b
		_ = cmd.Run()
		homeDir := strings.Split(b.String(), ":")[5]
		sshDir := fmt.Sprintf("%s/.ssh", homeDir)
		authKeysFile := fmt.Sprintf("%s/authorized_keys", sshDir)
		file, err := openOrCreate(authKeysFile)
		if err != nil {
			log.Debugf("Error while opening %s: %v", authKeysFile, err)
		}
		defer file.Close()
		_, err = file.WriteString(fmt.Sprintln(strings.Join(u.SSHAuthorizedKeys, "\n")))
		if err != nil {
			log.Debugf("Error writing keys in %s: %v", authKeysFile, err)
		}
	}

	// finally unlock
	cmd = exec.Command("passwd", "-u", u.Name)
	_ = cmd.Run()

	return nil
}
