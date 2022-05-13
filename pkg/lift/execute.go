package lift

import (
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mitchellh/go-ps"
	"github.com/moby/sys/mountinfo"
	log "github.com/sirupsen/logrus"
)

const (
	drpcliBin      = "/usr/local/bin/drpcli"
	drpcliRCFile   = "/etc/init.d/drpcli"
	chronyConfFile = "/etc/chrony/chrony.conf"
	ssmtpConfFile  = "/etc/ssmtp/ssmtp.conf"
)

var (
	fsPackage = map[string]string{
		"xfs":   "xfsprogs",
		"btrfs": "btrfs-progs",
		"ext2":  "e2fsprogs",
		"ext3":  "e2fsprogs",
		"ext4":  "e2fsprogs",
		"jfs":   "jfsutils",
		"ntfs":  "ntfs-3g-progs",
	}
)

// executes the `hostname` command, if hostname was provided in alpine-data
func (l *Lift) setHostname() error {
	if l.Data.Network.HostName != "" {
		host := strings.Split(l.Data.Network.HostName, ".")[0]

		cmd := exec.Command("hostname", host)
		if err := cmd.Run(); err != nil {
			return err
		}

		cmd = exec.Command("setup-hostname", "-n", host)
		if err := cmd.Run(); err != nil {
			return err
		}

		file, err := openOrCreate("/etc/hosts")
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err = file.WriteString(fmt.Sprintf("127.0.0.1\t%s %s\n", l.Data.Network.HostName, host)); err != nil {
			return err
		}
	}
	return nil
}

// mtaSetup installs and configures ssmtp as MTA
func (l *Lift) mtaSetup() error {
	if l.Data.MTA == nil {
		log.Debug("No MTA configured")
		return nil
	}

	log.Debug("apk add ssmtp")
	cmd := exec.Command("apk", "add", "ssmtp")
	if err := cmd.Run(); err != nil {
		return err
	}

	log.Debug("Generating ssmtp.conf")
	ssmtp, err := generateFileFromTemplate(*ssmtpConf, l.Data)
	if err != nil {
		return err
	}

	log.Debugf("Copying ssmtp.conf to %s", ssmtpConfFile)
	cmd = exec.Command("mv", ssmtp, ssmtpConfFile)
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

// executes the setup-disk script if scratch disk is set
// It tries to detect if Docker is running, since Docker will
// mount /var/lib/docker, which prevents the scratch disk
// from being mounted correctly.
func (l *Lift) scratchDiskSetup() error {
	if l.Data.ScratchDisk == "" {
		log.Debug("No Scratch Disk defined")
		return nil
	}

	log.Debug("Check if Docker is running")
	// Give Docker some time to start
	time.Sleep(3 * time.Second)
	dockerPresent := false
	procs, err := ps.Processes()
	if err != nil {
		return err
	}
	log.WithField("numprocs", len(procs)).Debug("Fetch process list")
	for _, p := range procs {
		log.Debugf("Process: %s", p.Executable())
		if strings.Contains(strings.ToLower(p.Executable()), "docker") {
			log.Debug("Docker process detected")
			dockerPresent = true
		}
	}

	if dockerPresent {
		log.Info("Stopping Docker...")
		_ = doService("docker", STOP)
		// Wait a little bit for Docker to stop
		time.Sleep(2 * time.Second)
	}

	mnts, _ := mountinfo.GetMounts(func(mnt *mountinfo.Info) (skip, stop bool) {
		if strings.Contains(mnt.Mountpoint, "/var") {
			return false, false
		}
		return true, false
	})
	for _, mnt := range mnts {
		log.Infof("Unmounting %s", mnt.Mountpoint)
		cmd := exec.Command("umount", mnt.Mountpoint)
		_ = cmd.Run()
	}

	log.WithField("disk", l.Data.ScratchDisk).Debug("Setup Scratch Disk")
	cmd := exec.Command("setup-disk", "-q", "-m", "data", l.Data.ScratchDisk)

	// If not silenced, show setup-alpine output on stdout
	if !silent {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	env := append(os.Environ(), "VARFS=xfs")
	env = append(env, fmt.Sprintf("ERASE_DISKS=%s", l.Data.ScratchDisk))
	env = append(env, "MKFS_OPTS_VAR=-f")
	env = append(env, "DEFAULT_DISK=none")
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return err
	}

	if dockerPresent {
		log.Info("Starting Docker...")
		_ = doService("docker", START)
	}

	// Check if swap was re-enabled
	out, err := exec.Command("cat", "/proc/swap").Output()
	if err != nil {
		return nil
	}
	if !strings.Contains(string(out), l.Data.ScratchDisk) {
		// just try, don't care about the result since we can't fix it here..
		_ = exec.Command("swapon", "-a").Run()
	}

	return nil
}

// Encrypt, Format and mount other disks if configured
func (l *Lift) diskSetup() error {
	if l.Data.Disks == nil {
		log.Debug("No additional disks")
		return nil
	}
	for i, disk := range l.Data.Disks {
		log.Debug("Installing cryptsetup package")
		_ = exec.Command("apk", "add", "--no-cache", "cryptsetup").Run()
		log.Debug("Generating random key")
		rand.Seed(time.Now().UnixNano())
		letterRunes := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
		b := make([]rune, 30)
		for i := range b {
			b[i] = letterRunes[rand.Intn(len(letterRunes))]
		}
		luksPass := string(b)
		log.Debugf("Encrypting %s (LUKS)", disk.Device)
		cmdStr := fmt.Sprintf("echo -n '%s' | cryptsetup luksFormat %s -", luksPass, disk.Device)
		encryptCmd := exec.Command("ash", "-c", cmdStr)
		encryptCmd.Stdout = os.Stdout
		if err := encryptCmd.Run(); err != nil {
			return err
		}

		if log.GetLevel() == log.DebugLevel {
			dumpCmd := exec.Command("cryptsetup", "luksDump", disk.Device)
			dumpCmd.Stdout = os.Stdout
			_ = dumpCmd.Run()
		}

		mapper := fmt.Sprintf("crypt%d", i)
		log.Debugf("Opening %s as %s", disk.Device, mapper)
		cmdStr = fmt.Sprintf("echo -n '%s' | cryptsetup luksOpen %s %s -d -", luksPass, disk.Device, mapper)
		openCmd := exec.Command("ash", "-c", cmdStr)
		openCmd.Stdout = os.Stdout
		if err := openCmd.Run(); err != nil {
			return err
		}

		// Check filesystem support and kernel modules. Ignore exit codes..
		log.Debugf("Checking filesystem prerequisites")
		_ = exec.Command("apk", "add", "--no-cache", fsPackage[strings.ToLower(disk.FileSystemType)]).Run()
		_ = exec.Command("modprobe", strings.ToLower(disk.FileSystemType)).Run()

		mapdevice := fmt.Sprintf("/dev/mapper/%s", mapper)
		log.Debugf("Creating %s filesystem on %s", disk.FileSystemType, mapdevice)
		cmd := exec.Command(fmt.Sprintf("mkfs.%s", strings.ToLower(disk.FileSystemType)), mapdevice)
		if err := cmd.Run(); err != nil {
			return err
		}
		log.Debugf("Creating mountpoint %s", disk.MountPoint)
		cmd = exec.Command("mkdir", "-p", disk.MountPoint)
		if err := cmd.Run(); err != nil {
			return err
		}
		log.Debugf("Mounting %s on %s as %s", mapdevice, disk.MountPoint, disk.FileSystemType)
		cmd = exec.Command("mount", "-t", strings.ToLower(disk.FileSystemType), mapdevice, disk.MountPoint)
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

// configures the network interface(s)
func (l *Lift) networkSetup() error {
	var cmd *exec.Cmd

	if l.Data.Network.InterfaceOpts == "" {
		// Do auto config
		log.Debug("No interface specification defined; auto-config")
		cmd = exec.Command("setup-interfaces", "-a")
	} else {
		log.Debug("Apply interface specification")
		cmd = exec.Command("setup-interfaces", "-i")
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		io.WriteString(stdin, l.Data.Network.InterfaceOpts)
		stdin.Close()
	}

	if err := cmd.Run(); err != nil {
		return err
	}

	if err := doService("networking", RESTART); err != nil {
		log.Infof("%v", err)
	}

	return nil
}

// sets the proxy
func (l *Lift) proxySetup() error {
	if l.Data.Network.Proxy != "" {
		log.WithField("proxy", l.Data.Network.Proxy).Debug("Found proxy setting")
		cmd := exec.Command("setup-proxy", l.Data.Network.Proxy)
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

// sets root password if needed
func (l *Lift) rootPasswdSetup() error {
	// Always set a password, randomized if empty..
	if l.Data.RootPasswd == "" {
		rand.Seed(time.Now().UnixNano())
		letterRunes := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789&^%$#-_")
		b := make([]rune, 30)
		for i := range b {
			b[i] = letterRunes[rand.Intn(len(letterRunes))]
		}
		l.Data.RootPasswd = string(b)
	}
	chpasswdCmd := exec.Command("chpasswd")
	reader, writer := io.Pipe()
	s := []byte(fmt.Sprintf("root:%s\n", l.Data.RootPasswd))

	chpasswdCmd.Stdout = os.Stdout
	chpasswdCmd.Stderr = os.Stderr
	chpasswdCmd.Stdin = reader
	chpasswdCmd.Start()
	writer.Write(s)
	writer.Close()
	err := chpasswdCmd.Wait()
	reader.Close()
	if err != nil {
		return err
	}
	return nil

}

// parses sshd_config, writes authorized_keys file and restarts sshd service
func (l *Lift) sshdSetup() error {
	if l.Data.SSHDConfig == nil {
		return nil
	}
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

// call setup-dns Alpine setup script for configuring resolv.conf
func (l *Lift) dnsSetup() error {
	if l.Data.Network.ResolvConf != nil {
		if l.Data.Network.ResolvConf.NameServers != nil && len(l.Data.Network.ResolvConf.NameServers) > 0 {
			cmd := exec.Command("setup-dns", "-d", l.Data.Network.ResolvConf.Domain, "-n", strings.Join(l.Data.Network.ResolvConf.NameServers, " "))
			if err := cmd.Run(); err != nil {
				return err
			}
		}
	}
	return nil
}

// call setup-ntp Alpine setup script for configuring NTP
func (l *Lift) ntpSetup() error {
	if l.Data.Network.NTP != nil {
		if (l.Data.Network.NTP.Pools != nil && len(l.Data.Network.NTP.Pools) > 0) ||
			(l.Data.Network.NTP.Servers != nil && len(l.Data.Network.NTP.Servers) > 0) {
			cmd := exec.Command("setup-ntp", "-c", "chrony")
			if err := cmd.Run(); err != nil {
				return err
			}
			log.Debug("Generating chrony.conf")
			chrony, err := generateFileFromTemplate(*chronyConf, l.Data)
			if err != nil {
				return err
			}
			log.Debugf("Copying chrony.conf to %s", chronyConfFile)
			cmd = exec.Command("mv", chrony, chronyConfFile)
			if err := cmd.Run(); err != nil {
				return err
			}
			log.Debug("Restart Chrony")
			_ = doService("chronyd", RESTART)
		}
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
		drpcli, err := downloadFile(url, nil)
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
	if l.Data.Packages == nil {
		return nil
	}
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
			if data, err = downloadFile(wf.ContentURL, nil); err != nil {
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
