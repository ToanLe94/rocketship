package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"strings"

	"github.com/jinzhu/gorm"
)

const (
	ModeDHCP   = "dhcp"
	ModeStatic = "static"

	// When DHCP server provides us DNS entries, how to treat them
	ModeAppend   = "append"
	ModePrepend  = "prepend"
	ModeOverride = "override" // supercede, really

	// Default options we request from the dhcp server
	DefaultSendOptionsJSON    = "{\"hostname\": \"gethostname()\"}"
	DefaultTimingOptionsJSON  = "{\"timeout\": \"10\", \"retry\": \"10\"}"
	DefaultRequireOptionsJSON = "[\"subnet-mask\"]"
	DefaultRequestOptionsJSON = "[\"subnet-mask\", \"broadcast-address\", \"time-offset\", " +
		"\"routers\", \"domain-name\", \"domain-name-servers\", \"domain-search\", " +
		"\"host-name\", \"netbios-name-servers\", \"netbios-scope\", \"interface-mtu\", " +
		"\"rfc3442-classless-static-routes\", \"ntp-servers\", \"dhcp6.domain-search\", " +
		"\"dhcp6.fqdn\", \"dhcp6.name-servers\", \"dhcp6.sntp-servers\"]"
)

//
// File generators
//

// RewriteInterfacesFile rewrites the network interfaces configuration file.
func (c *Controller) RewriteInterfacesFile() error {
	str, err := c.interfacesConfigFileContents()
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(InterfacesFilePath, []byte(str), 0644)
	if err != nil {
		return err
	}

	return nil
}

// RewriteDhClientConf file rewrites the dhclient.conf configuration file.
func (c *Controller) RewriteDhclientConfFile() error {
	str, err := c.dhclientConfFileContents()
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(DhclientConfFilePath, []byte(str), 0644)
	if err != nil {
		return err
	}

	return nil
}

//
// Helpers
//

// returns the contents of the interfaces file
func (c *Controller) interfacesConfigFileContents() ([]byte, error) {
	contents := bytes.Buffer{}

	ifaces := []InterfaceConfig{}
	err := c.db.Find(&ifaces).Error
	if err != nil {
		return contents.Bytes(), err
	}

	// Banner
	contents.WriteString("# This file is AUTOGENERATED.\n")
	contents.WriteString("#\n\n")
	// static section for 'lo'
	contents.WriteString("auto lo\n")
	contents.WriteString("iface lo inet loopback\n\n")

	for _, iface := range ifaces {
		contents.WriteString(c.interfacesConfigFileSection(iface))
		contents.WriteString("\n")
	}

	return contents.Bytes(), nil
}

// returns a section of the interfaces config file that configures the specified nic.
func (c *Controller) interfacesConfigFileSection(iface InterfaceConfig) string {
	contents := bytes.Buffer{}

	// per the docs, err is always nil
	contents.WriteString("auto " + iface.Name + "\n")
	contents.WriteString("iface " + iface.Name + " inet " + iface.Mode + "\n")
	if iface.Mode == ModeStatic {
		contents.WriteString("address " + iface.Address + "\n")
		contents.WriteString("netmask " + iface.Netmask + "\n")
		contents.WriteString("gateway " + iface.Gateway + "\n")
	}

	return string(contents.Bytes())
}

// returns contents of the dhclient.conf file
func (c *Controller) dhclientConfFileContents() ([]byte, error) {
	ret := bytes.Buffer{}

	headerLines := []string{
		"# This file is autogenerated. Do not edit this file.",
		"# Your changes will be overwritten.",
		"#",
		"",
		"option rfc3442-classless-static-routes code 121 = array of unsigned integer 8;",
		"",
	}

	globalOptions := []string{
		"# backoff-cutoff 2;",
		"# initial-interval 1;",
		"# link-timeout 10;",
		"# reboot 0;",
		"# retry 10;",
		"# select-timeout 0;",
		"# timeout 30;",
		"",
	}

	// Add header lines
	for _, h := range headerLines {
		ret.WriteString(h)
		ret.WriteString("\n")
	}

	// Add global options
	for _, g := range globalOptions {
		ret.WriteString(g)
		ret.WriteString("\n")
	}

	// Add interface specific options
	ifaces := []InterfaceConfig{}
	c.db.Where(InterfaceConfig{Mode: ModeDHCP}).Find(&ifaces)
	for _, iface := range ifaces {
		if str, err := c.dhconfFileSection(iface); err != nil {
			fmt.Errorf("ERROR:", err)
			// TODO: LOG
			return []byte{}, err
		} else {
			ret.WriteString(str)
		}
	}

	return ret.Bytes(), nil
}

func (c *Controller) dhconfFileSection(iface InterfaceConfig) (string, error) {
	var (
		ret         = bytes.Buffer{}
		dhcpProfile = DHCPProfile{}

		err error
	)

	err = c.db.First(&dhcpProfile, iface.DHCPProfileID).Error
	if err != nil {
		return "", err
	}

	sectionForSlice := func(indent int, clause string, elems []string) string {
		var (
			lines     = []string{}
			indentStr = strings.Repeat(" ", indent)
			chunks    = chunkSlice(elems, 3)
		)
		for _, chunk := range chunks {
			lines = append(lines, strings.Join(chunk, ", "))
		}
		return indentStr + clause + " " +
			strings.Join(lines, "\n"+indentStr+strings.Repeat(" ", len(clause)+1)) + ";\n"
	}

	sectionForMap := func(indent int, clause string, elems map[string]string) string {
		var (
			retbuf    = bytes.Buffer{}
			indentStr = strings.Repeat(" ", indent)
		)

		for k, v := range elems {
			if len(clause) > 0 {
				retbuf.WriteString(fmt.Sprintf("%s%s %s %s;\n", indentStr, clause, k, v))
			} else {
				retbuf.WriteString(fmt.Sprintf("%s%s %s;\n", indentStr, k, v))
			}
		}
		return string(retbuf.Bytes())
	}

	decodeMap := func(ser string) map[string]string {
		retmap := make(map[string]string)
		if len(ser) <= 0 {
			return retmap
		}
		err := json.Unmarshal([]byte(ser), &retmap)
		if err != nil {
			// TODO: log
			fmt.Println("ERROR (", ser, "):", err)
		}
		return retmap
	}

	decodeSlice := func(ser string) []string {
		ret := []string{}
		if len(ser) <= 0 {
			return ret
		}
		err := json.Unmarshal([]byte(ser), &ret)
		if err != nil {
			// TODO: log
			fmt.Println("ERROR (", ser, "):", err)
		}
		return ret
	}

	ret.WriteString(fmt.Sprintf("interface %s {\n", iface.Name))

	// TODO: handle the 'special' HostNameMode and DomainNameMode flags which allow the user to
	// easily specify whether to override the hostname and domain name returned by the server.

	// Timing options are not 'named'
	ret.WriteString(sectionForMap(2, "", decodeMap(dhcpProfile.TimingOptions)))
	ret.WriteString(sectionForMap(2, "send", decodeMap(dhcpProfile.SendOptions)))
	ret.WriteString(sectionForSlice(2, "request", decodeSlice(dhcpProfile.RequestOptions)))
	ret.WriteString(sectionForSlice(2, "require", decodeSlice(dhcpProfile.RequireOptions)))

	// Not configurable yet (see models.go)
	//ret.WriteString(sectionForMap(2, "append", decodeMap(dhcpProfile.AppendOptions)))
	//ret.WriteString(sectionForMap(2, "prepend", decodeMap(dhcpProfile.PrependOptions)))
	//ret.WriteString(sectionForMap(2, "supersede", decodeMap(dhcpProfile.SupersedeOptions)))

	ret.WriteString("}\n")

	return string(ret.Bytes()), nil
}

//
// DB Models
//

type DHCPProfile struct {
	ID            int64
	TimingOptions string // Serialized json map[string]string
	SendOptions   string // Serialized json map[string]string

	// Not yet supported
	// AppendOptions    string // Serialized json map[string]string
	// PrependOptions   string // Serialized json map[string]string
	// SupersedeOptions string // Serialized json map[string]string

	DNSMode            string // One of Mode[Append|Prepend|Supercede]
	OverrideHostname   bool   // Whether to supercede the name returned by the dhcp server
	OverrideDomainName bool   // Whether to supercede the name returned by the dhcp server

	RequireOptions string // OptionsSeparator separated string
	RequestOptions string // OptionsSeparator separated string
}

type InterfaceConfig struct {
	ID      int64
	Name    string
	Enabled bool
	Mode    string

	Address string
	Gateway string
	Netmask string

	DHCPProfileID int64
}

func (i *InterfaceConfig) BeforeSave(txn *gorm.DB) error {
	switch i.Mode {
	case ModeStatic:
		return i.validateIPs()
	case ModeDHCP:
		return i.validateDHCPProfile(txn)
	default:
		return fmt.Errorf("Invalid mode (%s) set for interface %s", i.Mode, i.Name)
	}
	return nil
}

func (d *DHCPProfile) BeforeCreate(txn *gorm.DB) error {
	if len(d.RequestOptions) <= 0 {
		txn.Model(d).Update(DHCPProfile{
			TimingOptions:  DefaultTimingOptionsJSON,
			SendOptions:    DefaultSendOptionsJSON,
			RequestOptions: DefaultRequestOptionsJSON,
			RequireOptions: DefaultRequireOptionsJSON,
		})
	}
	return nil
}

func (d *DHCPProfile) BeforeDelete(txn *gorm.DB) error {
	ifaces := []InterfaceConfig{}
	err := txn.Where(InterfaceConfig{DHCPProfileID: d.ID}).Find(&ifaces).Error
	if err != nil {
		return err
	}

	if len(ifaces) > 0 {
		return fmt.Errorf("Cannot delete profile, %s is still using it", ifaces[0].Name)
	}

	return nil
}

func (i *InterfaceConfig) validateDHCPProfile(txn *gorm.DB) error {
	if i.Mode == ModeDHCP {
		dp := DHCPProfile{}
		if err := txn.Find(&dp, i.DHCPProfileID).Error; err != nil {
			return fmt.Errorf("Cannot save interface %s (id:%d) with DHCP profile %d",
				i.Name, i.ID, i.DHCPProfileID)
		}
	}
	return nil
}

func (i *InterfaceConfig) validateIPs() error {
	addrs := []struct {
		ipstr string
		name  string
	}{
		{ipstr: i.Address, name: "IP"},
		{ipstr: i.Gateway, name: "Gateway"},
		{ipstr: i.Netmask, name: "Netmask"},
	}

	for _, addr := range addrs {
		a := net.ParseIP(addr.ipstr)
		if a == nil {
			return fmt.Errorf("Invalid %s address", addr.name)
		}
	}

	// validate netmask
	nm := net.IPMask(net.ParseIP(i.Netmask).To4())
	ones, bits := nm.Size()
	if ones == 0 && bits == 0 {
		return fmt.Errorf("Invalid netmask (%s)", i.Netmask)
	}

	// ensure gateway is within the network defined by addressr+netmask
	ipnet := net.IPNet{IP: net.ParseIP(i.Address), Mask: nm}
	if !ipnet.Contains(net.ParseIP(i.Gateway)) {
		return fmt.Errorf("Gateway %s is not on network (addr: %s mask %s)",
			i.Gateway, i.Address, i.Netmask)
	}

	return nil
}

//
// Resources
//

type DHCPProfileResource struct {
	DNSMode            string // One of Mode[None|Append|Prepend|Supercede]
	OverrideHostname   bool   // Whether to supercede the name returned by the dhcp server
	OverrideDomainName bool   // Whether to supercede the name returned by the dhcp server

	RequireOptions []string // OptionsSeparator separated string
	RequestOptions []string // OptionsSeparator separated string
}

func (r *DHCPProfileResource) FromDHCPProfileModel(d DHCPProfile) error {
	var (
		deserializedRequestOpts = []string{}
		deserializedRequireOpts = []string{}
	)

	if len(d.RequestOptions) > 0 {
		err := json.Unmarshal([]byte(d.RequestOptions), &deserializedRequestOpts)
		if err != nil {
			return err
		}
	}
	if len(d.RequireOptions) > 0 {
		err := json.Unmarshal([]byte(d.RequireOptions), &deserializedRequireOpts)
		if err != nil {
			return err
		}
	}

	r.DNSMode = d.DNSMode
	r.OverrideHostname = d.OverrideHostname
	r.OverrideDomainName = d.OverrideDomainName
	r.RequestOptions = deserializedRequestOpts
	r.RequireOptions = deserializedRequireOpts

	return nil
}

func (r DHCPProfileResource) ToDHCPProfileModel() (DHCPProfile, error) {

	serializedRequestOpts, err := json.Marshal(r.RequestOptions)
	if err != nil {
		return DHCPProfile{}, err
	}
	serializedRequireOpts, err := json.Marshal(r.RequireOptions)
	if err != nil {
		return DHCPProfile{}, err
	}
	if r.DNSMode != ModeAppend && r.DNSMode != ModePrepend && r.DNSMode != ModeOverride {
		return DHCPProfile{}, fmt.Errorf("Invalid DNSMode")
	}

	return DHCPProfile{
		DNSMode:            r.DNSMode,
		OverrideHostname:   r.OverrideHostname,
		OverrideDomainName: r.OverrideDomainName,
		RequireOptions:     string(serializedRequireOpts),
		RequestOptions:     string(serializedRequestOpts),

		//AppendOptions:    "{}",
		//PrependOptions:   "{}",
		//SupercedeOptions: "{}",
	}, nil
}

//
// DB Seed
//

func SeedInterface(db *gorm.DB) {
	var (
		profile = DHCPProfile{ID: 1}
		iface   = InterfaceConfig{Name: "eth0", Mode: ModeDHCP, DHCPProfileID: 1}
	)

	// Ensure latest schema
	db.AutoMigrate(&DHCPProfile{})
	db.AutoMigrate(&InterfaceConfig{})

	db.FirstOrCreate(&profile, profile)
	db.FirstOrCreate(&iface, iface)
}
