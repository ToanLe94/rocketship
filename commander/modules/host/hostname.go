package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/amoghe/go-upstart"
	"github.com/jinzhu/gorm"
	"github.com/zenazn/goji/web"
)

const (
	// Minimum length of hostname string
	MinHostnameLength = 1

	// Default hostname for the system
	DefaultHostname = "ncc1701"
)

//
// Endpoint handlers
//

func (c *Controller) GetHostname(ctx web.C, w http.ResponseWriter, r *http.Request) {
	host := Hostname{}
	err := c.db.First(&host, 1).Error
	if err != nil {
		c.jsonError(err, w)
		return
	}

	bytes, err := json.Marshal(&host)
	if err != nil {
		c.jsonError(err, w)
		return
	}

	_, err = w.Write(bytes)
	if err != nil {
		c.jsonError(err, w)
		return
	}

	return
}

func (c *Controller) PutHostname(ctx web.C, w http.ResponseWriter, r *http.Request) {
	host := Hostname{ID: 1}

	bodybytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		c.jsonError(err, w)
		return
	}

	err = json.Unmarshal(bodybytes, &host)
	if err != nil {
		c.jsonError(err, w)
		return
	}
	host.ID = 1

	err = c.db.Save(&host).Error
	if err != nil {
		err = fmt.Errorf("Failed to persist configuration (%s)", err)
		c.jsonError(err, w)
		return
	}

	applicator := func() error {
		if err := c.RewriteHostnameFile(); err != nil {
			return err
		}
		if err := upstart.RestartJob("hostname"); err != nil {
			return err
		}
		return nil
	}

	if _, there := ctx.Env[NoApplyEnvKey]; there {
		c.log.Infoln("Skipping apply hostname to system (\"noapply\" present in env)")
	} else {
		c.log.Infoln("Applying hostname to system")
		if err := applicator(); err != nil {
			c.log.Warningln("failed to apply hostname to system: ", err)
		}
	}

	w.Write(bodybytes)
}

//
// File generators
//

// RewriteHostnameFile rewrites the hostname file.
func (c *Controller) RewriteHostnameFile() error {
	c.log.Infoln("Rewriting hostname file")

	contents, err := c.hostnameFileContents()
	if err != nil {
		return err
	}

	err = ioutil.WriteFile("/etc/hostname", contents, 0644)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) RewriteEtcHostsFile() error {
	c.log.Infoln("Rewriting etc/hosts file")

	contents, err := c.etcHostsFileContents()
	if err != nil {
		return err
	}

	err = ioutil.WriteFile("/etc/hosts", contents, 0644)
	if err != nil {
		return err
	}

	return nil
}

// returns contents of the hostname file.
func (c *Controller) hostnameFileContents() ([]byte, error) {
	host := Hostname{}

	err := c.db.First(&host, 1).Error
	if err != nil {
		return nil, err
	}

	return []byte(host.Hostname + "\n"), nil
}

func (c *Controller) etcHostsFileContents() ([]byte, error) {
	var (
		ret      = bytes.Buffer{}
		host     = Hostname{}
		dom      = Domain{}
		hostname = DefaultHostname
		fqdn     = ""
	)

	c.db.First(&host, 1)
	if len(host.Hostname) > 0 {
		hostname = host.Hostname
	}

	c.db.First(&dom, 1)
	if len(dom.Domain) > 0 {
		fqdn = hostname + dom.Domain
	}

	lines := []string{
		"# This file is autogenerated. Do not edit this file.",
		"# Your changes will be overwritten.",
		"127.0.0.1 localhost",
		fmt.Sprintf("127.0.1.1 %s %s", hostname, fqdn),
		"",
		"# IPv6",
		"::1     ip6-localhost ip6-loopback",
		"fe00::0 ip6-localnet",
		"ff00::0 ip6-mcastprefix",
		"ff02::1 ip6-allnodes",
		"ff02::2 ip6-allrouters",
	}

	// Add header lines
	for _, h := range lines {
		ret.WriteString(h)
		ret.WriteString("\n")
	}

	return ret.Bytes(), nil
}

//
// DB Models
//

type Hostname struct {
	ID       int64 `json:"-"`
	Hostname string
}

func (h *Hostname) BeforeSave(txn *gorm.DB) error {
	// Length check
	if len(h.Hostname) < MinHostnameLength {
		return fmt.Errorf("Hostname cannot be shorter than %d chars", MinHostnameLength)
	}
	// Invalid chars check
	for _, char := range []string{" ", ".", "/"} {
		if strings.Contains(h.Hostname, char) {
			return fmt.Errorf("Hostname cannot contain %s", char)
		}
	}
	return nil
}

//
// Resource
//

type HostnameResource struct {
	Hostname string
}

func (h HostnameResource) ToHostnameModel() Hostname {
	return Hostname{Hostname: h.Hostname}
}

func (h *HostnameResource) FromHostnameModel(m Hostname) {
	h.Hostname = m.Hostname
}

//
// DB Seed
//

func (c *Controller) seedHostname() {
	c.log.Infoln("Seeding hostname")
	c.db.FirstOrCreate(&Hostname{Hostname: DefaultHostname})
}
