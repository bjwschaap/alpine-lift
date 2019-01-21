package lift

import (
	"strconv"
)

type AlpineData struct {
	RootPasswd string          `yaml:"password"`
	Network    NetworkSettings `yaml:"network"`
	Packages   PackagesConfig  `yaml:"packages"`
	DRP        DRProvision     `yaml:"dr_provision"`
	SSHDConfig SSHD            `yaml:"sshd"`
	Groups     MultiString     `yaml:"groups"`
	Users      []User          `yaml:"users"`
	RunCMD     []MultiString   `yaml:"runcmd"`
	WriteFiles []WriteFile     `yaml:"write_files"`
	TimeZone   string          `yaml:"timezone"`
	Keymap     string          `yaml:"keymap"`
}

type User struct {
	Name              string      `yaml:"name"`
	Description       string      `yaml:"gecos"`
	HomeDir           string      `yaml:"homedir"`
	Shell             string      `yaml:"shell"`
	NoCreateHomeDir   bool        `yaml:"no_create_homedir"`
	PrimaryGroup      string      `yaml:"primary_group"`
	Groups            MultiString `yaml:"groups"`
	System            bool        `yaml:"system"`
	SSHAuthorizedKeys []string    `yaml:"ssh_authorized_keys"`
	Password          string      `yaml:"passwd"`
}

type SSHD struct {
	Port                   int      `yaml:"port"`
	ListenAddress          string   `yaml:"listen_address"`
	AuthorizedKeys         []string `yaml:"authorized_keys"`
	PermitRootLogin        bool     `yaml:"permit_root_login"`
	PermitEmptyPasswords   bool     `yaml:"permit_empty_passwords"`
	PasswordAuthentication bool     `yaml:"password_authentication"`
}

type DRProvision struct {
	InstallRunner bool   `yaml:"install_runner"`
	AssetsURL     string `yaml:"assets_url"`
	Token         string `yaml:"token"`
	Endpoint      string `yaml:"endpoint"`
	UUID          string `yaml:"uuid"`
}

type NetworkSettings struct {
	HostName      string              `yaml:"hostname"`
	InterfaceOpts string              `yaml:"interfaces"`
	ResolvConf    ResolvConfiguration `yaml:"resolv_conf"`
	Proxy         string              `yaml:"proxy"`
}

type ResolvConfiguration struct {
	NameServers   MultiString `yaml:"nameservers"`
	SearchDomains MultiString `yaml:"search_domains"`
	Domain        string      `yaml:"domain"`
}

type PackagesConfig struct {
	Repositories MultiString `yaml:"repositories"`
	Update       bool        `yaml:"update"`
	Upgrade      bool        `yaml:"upgrade"`
	Install      MultiString `yaml:"install"`
	Uninstall    MultiString `yaml:"uninstall"`
}

type WriteFile struct {
	Encoding    string `yaml:"encoding"`
	Content     string `yaml:"content"`
	Path        string `yaml:"path"`
	Owner       string `yaml:"owner"`
	Permissions string `yaml:"permissions"`
}

// type alias
type MultiString []string

// custom unmarshalling function for parsing yaml values that
// contains one or more string (either string or array of strings)
// but always returning []string (aliased with MultiString)
func (ms *MultiString) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var values []string
	err := unmarshal(&values)
	if err != nil {
		var s string
		err := unmarshal(&s)
		if err != nil {
			return err
		}
		*ms = []string{s}
	} else {
		*ms = values
	}
	return nil
}

// Initialize alpine-data with sane defaults
func InitAlpineData() *AlpineData {
	return &AlpineData{
		RootPasswd: "alpine",
		TimeZone:   "UTC",
		Keymap:     "us us",
		Network: NetworkSettings{
			HostName: "alpine",
			InterfaceOpts: `auto lo
iface lo inet loopback
			
auto eth0
iface eth0 inet dhcp
	hostname alpine
`,
			Proxy: "none",
			ResolvConf: ResolvConfiguration{
				NameServers: []string{"8.8.8.8"},
				Domain:      "example.com",
			},
		},
		SSHDConfig: SSHD{
			Port:                   22,
			ListenAddress:          "0.0.0.0",
			PermitRootLogin:        true,
			PermitEmptyPasswords:   false,
			PasswordAuthentication: false,
		},
		DRP: DRProvision{
			InstallRunner: true,
		},
		Packages: PackagesConfig{
			Repositories: []string{
				"http://dl-cdn.alpinelinux.org/alpine/v3.8/main",
				"http://dl-cdn.alpinelinux.org/alpine/v3.8/community",
			},
		},
	}
}

// Returns a key-value map with SSH settings from alpine-data
func (l *Lift) getSSHDKVMap() map[string]string {
	return map[string]string{
		"Port":                   strconv.Itoa(l.Data.SSHDConfig.Port),
		"ListenAddress":          l.Data.SSHDConfig.ListenAddress,
		"PermitRootLogin":        boolToYesNo(l.Data.SSHDConfig.PermitRootLogin),
		"PermitEmptyPasswords":   boolToYesNo(l.Data.SSHDConfig.PermitEmptyPasswords),
		"PasswordAuthentication": boolToYesNo(l.Data.SSHDConfig.PasswordAuthentication),
	}
}

// Converts bool values to either "yes" or "no"
func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
