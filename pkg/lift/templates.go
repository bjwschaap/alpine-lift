package lift

import (
	"io/ioutil"
	"strings"
	"text/template"

	log "github.com/sirupsen/logrus"
)

const (
	answerFileTemplate = `KEYMAPOPTS="{{ .Keymap }}"
	{{ $h := split .Network.HostName "." -}}
	HOSTNAMEOPTS="-n {{ index $h 0 }}"
	INTERFACESOPTS="{{ .Network.InterfaceOpts }}"
	DNSOPTS="-d {{ .Network.ResolvConf.Domain }} {{range .Network.ResolvConf.NameServers}}{{.}}{{end}}"
	TIMEZONEOPTS="-z {{ .TimeZone }}"
	PROXYOPTS="{{ .Network.Proxy }}"
	APKREPOSOPTS="-1"
	SSHDOPTS="-c openssh"
	NTPOPTS="-c none"
	DISKOPTS={{- if .ScratchDisk -}}"-q -m data {{ .ScratchDisk -}}"{{- else -}}"none"{{- end }}
	LBUOPTS="none"
	APKCACHEOPTS="none"
`

	drpcliServiceTemplate = `#!/sbin/openrc-run
  
	name=drpcli
	pidfile="/run/${name}.pid"
	runfile="/run/openrc/started/${name}"
	logfile="/var/log/drpcli.log"
	
	depend() {
			need net
			use cgroups
			before sshd docker
	}
	
	start_pre() {
			if [ ! -f $pidfile ] ; then
				touch $pidfile || return 1
			fi
	
			if [ ! -f $logfile ] ; then
				touch $logfile || return 1
			fi
			/usr/local/bin/drpcli -E {{ .DRP.Endpoint }} machines update {{ .DRP.UUID }} '{"Runnable":true}' >> $logfile
	}
	
	stop_pre() {
			/usr/local/bin/drpcli -E {{ .DRP.Endpoint }} machines update {{ .DRP.UUID }} '{"Runnable":false}' >> $logfile
	}
	
	start() {
		ebegin "Starting drpcli"
		start-stop-daemon                \
			--start                      \
			--name "$name"               \
			--background                 \
			--quiet                      \
			--pidfile "$pidfile"         \
			--wait 3000                  \
			--progress                   \
			--exec /usr/local/bin/drpcli \
			--                           \
			-E {{ .DRP.Endpoint }} machines processjobs {{ .DRP.UUID }} >> $logfile
		eend $?
	}
	
	stop() {
		ebegin "Stopping drpcli"
		killall -3 $name
		rm -f $runfile
		eend 0
	}`

	repositoriesTemplate = "{{ range . }}{{ . }}\n{{ end }}"

	chronyTemplate = `{{ if .Network.NTP.Pools }}
{{ range .Network.NTP.Pools }}
pool {{.}} iburst maxsources 3
{{ end }}
initstepslew 10 {{ index .Network.NTP.Pools 0 }}
{{ end }}
{{ if .Network.NTP.Servers }}
{{ range .Network.NTP.Servers }}
server {{.}} iburst maxsources 3
{{ end }}
initstepslew 10 {{ index .Network.NTP.Servers 0 }}
{{ end }}
driftfile /var/lib/chrony/chrony.drift
rtcsync`

	ssmtpTemplate = `hostname={{ .Network.HostName }}
{{ if .MTA.Root }}root={{ .MTA.Root }}{{ end }}
{{ if .MTA.Server }}mailhub={{ .MTA.Server }}{{ end }}
{{ if .MTA.UseTLS }}UseTLS=Yes{{ end }}
{{ if .MTA.UseSTARTTLS }}UseSTARTTLS=Yes{{ end }}
{{ if .MTA.User }}AuthUser={{ .MTA.User }}{{ end }}
{{ if .MTA.Password }}AuthPass={{ .MTA.Password }}{{ end }}
{{ if .MTA.AuthMethod }}AuthMethod={{ upper .MTA.AuthMethod }}{{ end }}
{{ if .MTA.RewriteDomain }}rewriteDomain={{ .MTA.RewriteDomain }}{{ end }}
{{ if .MTA.FromLineOverride }}FromLineOverride=Yes{{ end }}
`
)

var (
	tplFuncMap                                              = make(template.FuncMap)
	answerFile, drpcliInit, repoFile, chronyConf, ssmtpConf *template.Template
)

func init() {
	// Initialise parser functions
	tplFuncMap["split"] = Split
	tplFuncMap["upper"] = Upper
	answerFile = template.Must(template.New("answerfile").Funcs(tplFuncMap).Parse(answerFileTemplate))
	drpcliInit = template.Must(template.New("drpcli").Funcs(tplFuncMap).Parse(drpcliServiceTemplate))
	repoFile = template.Must(template.New("repositories").Funcs(tplFuncMap).Parse(repositoriesTemplate))
	chronyConf = template.Must(template.New("chrony").Funcs(tplFuncMap).Parse(chronyTemplate))
	ssmtpConf = template.Must(template.New("ssmtp").Funcs(tplFuncMap).Parse(ssmtpTemplate))
}

// This function takes a template and data struct, executes (parses) the template
// and stores the result in a temporary file. Then it returns the path to the
// generated file or an error if there was one. So this is basically a wrapper
// for template.Execute, but using a file.
func generateFileFromTemplate(t template.Template, data interface{}) (string, error) {
	// generate temporary file
	tmpfile, err := ioutil.TempFile("", "lift-*")
	if err != nil {
		return "", err
	}
	defer tmpfile.Close()

	// execute the template, saving the result in the tempfile
	if err := t.Execute(tmpfile, data); err != nil {
		return "", err
	}

	log.WithFields(log.Fields{
		"template": t.Name(),
		"file":     tmpfile.Name(),
	}).Debug("parsed template to file")

	// return handle to the temp file
	return tmpfile.Name(), nil
}

// Split is a parser function that can be used from inside the template
func Split(s string, d string) []string {
	return strings.Split(s, d)
}

// Upper is a parser function that can be used from inside the template
func Upper(s string) string {
	return strings.ToUpper(s)
}
