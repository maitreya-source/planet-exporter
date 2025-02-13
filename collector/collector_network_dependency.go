// Copyright 2021 - williamchanrico@gmail.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package collector

import (
	"planet-exporter/collector/task/darkstat"
	"planet-exporter/collector/task/ebpf"
	"planet-exporter/collector/task/inventory"
	"planet-exporter/collector/task/socketstat"

	"github.com/prometheus/client_golang/prometheus"
)

// networkDependencyCollector on network dependency metrics
type networkDependencyCollector struct {
	serverProcesses *prometheus.Desc
	upstream        *prometheus.Desc
	downstream      *prometheus.Desc
	traffic         *prometheus.Desc
	ebpfTraffic     *prometheus.Desc
}

func init() {
	registerCollector("network_dependency", NewNetworkDependencyCollector)
}

// NewNetworkDependencyCollector service
// All metrics have current host's Hostgroup identified in the 'local_hostgroup' label
func NewNetworkDependencyCollector() (Collector, error) {
	return &networkDependencyCollector{
		serverProcesses: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "server_process"),
			"Server process that are listening on network interfaces",
			[]string{"local_hostgroup", "bind", "process_name", "port"}, nil,
		),
		traffic: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "traffic_bytes_total"),
			"Total network traffic with peers",
			[]string{"local_hostgroup", "direction", "remote_hostgroup", "remote_ip", "local_domain", "remote_domain"}, nil,
		),
		ebpfTraffic: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "ebpf_traffic_bytes_total"),
			"Total network traffic with peers from ebpf_exporter",
			[]string{"local_hostgroup", "direction", "remote_hostgroup", "remote_ip", "local_domain", "remote_domain"}, nil,
		),
		upstream: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "upstream"),
			"Upstream dependency of this machine",
			[]string{"local_hostgroup", "remote_hostgroup", "local_address", "remote_address", "port", "protocol", "process_name"}, nil,
		),
		downstream: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "downstream"),
			"Downstream dependency of this machine",
			[]string{"local_hostgroup", "remote_hostgroup", "local_address", "remote_address", "port", "protocol", "process_name"}, nil,
		),
	}, nil
}

// Update implements the Collector interface
func (c networkDependencyCollector) Update(ch chan<- prometheus.Metric) error {
	traffic := darkstat.Get()
	ebpf := ebpf.Get()
	serverProcesses, upstreams, downstreams := socketstat.Get()
	localInventory := inventory.GetLocalInventory()

	for _, m := range traffic {
		ch <- prometheus.MustNewConstMetric(c.traffic, prometheus.GaugeValue, m.Bandwidth,
			m.LocalHostgroup, m.Direction, m.RemoteHostgroup, m.RemoteIPAddr, m.LocalDomain, m.RemoteDomain)
	}
	for _, m := range ebpf {
		ch <- prometheus.MustNewConstMetric(c.ebpfTraffic, prometheus.GaugeValue, m.Bandwidth,
			m.LocalHostgroup, m.Direction, m.RemoteHostgroup, m.RemoteIPAddr, m.LocalDomain, m.RemoteDomain)
	}
	for _, m := range upstreams {
		ch <- prometheus.MustNewConstMetric(c.upstream, prometheus.GaugeValue, 1,
			m.LocalHostgroup, m.RemoteHostgroup, m.LocalAddress, m.RemoteAddress, m.Port, m.Protocol, m.ProcessName)
	}
	for _, m := range downstreams {
		ch <- prometheus.MustNewConstMetric(c.downstream, prometheus.GaugeValue, 1,
			m.LocalHostgroup, m.RemoteHostgroup, m.LocalAddress, m.RemoteAddress, m.Port, m.Protocol, m.ProcessName)
	}
	for _, m := range serverProcesses {
		ch <- prometheus.MustNewConstMetric(c.serverProcesses, prometheus.GaugeValue, 1,
			localInventory.Hostgroup, m.Bind, m.Name, m.Port)
	}

	return nil
}
