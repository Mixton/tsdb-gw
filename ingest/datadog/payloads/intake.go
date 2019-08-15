package payloads

import (
	"encoding/json"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"
	schema "github.com/grafana/metrictank/schema"
)

type PersistPayload struct {
	OrgID    int    `json:"org-id"` // used in rowKey
	Hostname string `json:"string"` // used in rowKey
	Raw      []byte `json:"raw"`
}

type DataDogIntakePayload struct {
	AgentVersion     string `json:"agentVersion"`
	UUID             string `json:"uuid"`
	OS               string `json:"os"`
	InternalHostname string `json:"internalHostname"`
	Python           string `json:"python"`
	SystemStats      struct {
		CPUCores  int      `json:"cpuCores"`
		Machine   string   `json:"machine"`
		Platform  string   `json:"platform"`
		Processor string   `json:"processor"`
		PythonV   string   `json:"pythonV"`
		MacV      []string `json:"macV"`
		NixV      []string `json:"nixV"`
		FbsdV     []string `json:"fbsdV"`
		WinV      []string `json:"winV"`
	} `json:"systemStats"`
	Meta struct {
		SocketHostname string   `json:"socket-hostname"`
		Timezones      []string `json:"timezones"`
		SocketFqdn     string   `json:"socket-fqdn"`
		Hostname       string   `json:"hostname"`
		EC2Hostname    string   `json:"ec2-hostname"`
		InstanceID     string   `json:"instance-id"`
		HostAlias      []string `json:"host_aliases"`
	} `json:"meta"`
	HostTags map[string]interface{} `json:"host-tags"`
	Tags     struct {
		System              []string `json:"system,omitempty"`
		GoogleCloudPlatform []string `json:"google cloud platform,omitempty"`
	}
	Gohai string `json:"gohai"`
	OrgID int    `json:"org-id"`
}

type Gohai struct {
	CPU struct {
		CacheSize            string `json:"cache_size"`
		CPUCores             string `json:"cpu_cores"`
		CPULogicalProcessors string `json:"cpu_logical_processors"`
		Family               string `json:"family"`
		Mhz                  string `json:"mhz"`
		Model                string `json:"model"`
		ModelName            string `json:"model_name"`
		Stepping             string `json:"stepping"`
		VendorID             string `json:"vendor_id"`
	} `json:"cpu"`
	Filesystem []struct {
		MountedOn string `json:"mounted_on"`
		Name      string `json:"name"`
	}
	Network struct {
		Ipaddress   string `json:"ipaddress"`
		Ipaddressv6 string `json:"ipaddressv6"`
		Macaddress  string `json:"macaddress"`
	} `json:"network"`
	Platform struct {
		GOOARCH          string `json:"GOOARCH"`
		GOOS             string `json:"GOOS"`
		GoV              string `json:"goV"`
		HardwarePlatform string `json:"hardware_platform"`
		Hostname         string `json:"hostname"`
		KernelName       string `json:"kernel_name"`
		KernelRelease    string `json:"kernel_release"`
		KernelVersion    string `json:"kernel_version"`
		Machine          string `json:"machine"`
		Os               string `json:"os"`
		Processor        string `json:"processor"`
		PythonV          string `json:"pythonV"`
	} `json:"platform"`
}

func (i *DataDogIntakePayload) GeneratePersistentMetrics() []*schema.MetricData {
	metrics := []*schema.MetricData{}

	metrics = append(metrics, &schema.MetricData{
		Name:  "system_info",
		Tags:  i.generateInfoTags(),
		Value: 1,
		OrgId: i.OrgID,
	})

	gohaiJSON := strings.Replace(i.Gohai, `\"`, `"`, -1)
	g := Gohai{}

	err := json.Unmarshal([]byte(gohaiJSON), &g)
	if err != nil {
		log.Errorf("unable to decode Gohai payload: %v", err)
		return metrics
	}

	metrics = append(metrics,
		&schema.MetricData{
			Name:  "system_platform_info",
			Tags:  g.generatePlatformTags(i.InternalHostname),
			Value: 1,
			OrgId: i.OrgID,
		},
		&schema.MetricData{
			Name: "system_network_info",
			Tags: []string{
				"hostname=" + i.InternalHostname,
				"ipaddress=" + g.Network.Ipaddress,
				"ipaddressv6=" + g.Network.Ipaddressv6,
				"macaddress=" + g.Network.Macaddress,
			},
			Value: 1,
			OrgId: i.OrgID,
		},
	)

	for _, fs := range g.Filesystem {
		tagset := []string{
			"hostname=" + i.InternalHostname,
			"name=" + fs.Name,
		}

		if fs.MountedOn != "" {
			tagset = append(tagset, "mountpoint="+fs.MountedOn)
		}
		metrics = append(metrics, &schema.MetricData{
			Name:  "system_filesystem_info",
			Tags:  tagset,
			Value: 1,
			OrgId: i.OrgID,
		})
	}

	return metrics
}

func (i *DataDogIntakePayload) generateInfoTags() []string {
	tags := []string{}

	if i.AgentVersion != "" {
		tags = append(tags, "agentVersion="+i.AgentVersion)
	}
	if i.Meta.Hostname != "" {
		tags = append(tags, "host="+i.Meta.Hostname)
	}
	if i.InternalHostname != "" {
		tags = append(tags, "hostname="+i.InternalHostname)
	}
	if i.SystemStats.Machine != "" {
		tags = append(tags, "machine="+i.SystemStats.Machine)
	}
	if i.OS != "" {
		tags = append(tags, "os="+i.OS)
	}
	if i.SystemStats.Processor != "" {
		tags = append(tags, "processor="+i.SystemStats.Processor)
	}
	if i.Meta.SocketFqdn != "" {
		tags = append(tags, "socket_fqdn="+i.Meta.SocketFqdn)
	}
	if i.Meta.SocketHostname != "" {
		tags = append(tags, "socket_hostname="+i.Meta.SocketHostname)
	}
	if i.Meta.InstanceID != "" {
		tags = append(tags, "instanceID="+i.Meta.InstanceID)
	}

	for _, t := range i.Tags.System {
		tSplit := strings.SplitN(t, ":", 2)
		if len(tSplit) == 0 {
			continue
		}
		if len(tSplit) == 1 {
			tags = append(tags, tSplit[0])
			continue
		}
		if tSplit[1] == "" {
			tags = append(tags, tSplit[0])
			continue
		}
		tags = append(tags, tSplit[0]+"="+tSplit[1])
	}

	for _, t := range i.Tags.GoogleCloudPlatform {
		tSplit := strings.SplitN(t, ":", 2)
		if len(tSplit) == 0 {
			continue
		}
		if len(tSplit) == 1 {
			tags = append(tags, tSplit[0])
			continue
		}
		if tSplit[1] == "" {
			tags = append(tags, tSplit[0])
			continue
		}
		tags = append(tags, tSplit[0]+"="+tSplit[1])
	}
	sort.Strings(tags)
	return tags
}

func (g *Gohai) generatePlatformTags(hostname string) []string {
	tags := []string{
		"hostname=" + hostname,
	}

	if g.Platform.GOOARCH != "" {
		tags = append(tags, "GOOARCH="+g.Platform.GOOARCH)
	}

	if g.Platform.GOOS != "" {
		tags = append(tags, "GOOS="+g.Platform.GOOS)
	}

	if g.Platform.GoV != "" {
		tags = append(tags, "GoV="+g.Platform.GoV)
	}

	if g.Platform.HardwarePlatform != "" {
		tags = append(tags, "HardwarePlatform="+g.Platform.HardwarePlatform)
	}

	if g.Platform.KernelName != "" {
		tags = append(tags, "kernel_name="+g.Platform.KernelName)
	}

	if g.Platform.KernelRelease != "" {
		tags = append(tags, "kernel_release="+g.Platform.KernelRelease)
	}

	if g.Platform.KernelVersion != "" {
		tags = append(tags, "kernel_version="+g.Platform.KernelVersion)
	}

	if g.Platform.Machine != "" {
		tags = append(tags, "machine="+g.Platform.Machine)
	}

	if g.Platform.Os != "" {
		tags = append(tags, "os="+g.Platform.Os)
	}

	if g.Platform.Processor != "" {
		tags = append(tags, "processor="+g.Platform.Processor)
	}

	if g.Platform.PythonV != "" {
		tags = append(tags, "pythonV="+g.Platform.PythonV)
	}

	sort.Strings(tags)
	return tags
}
