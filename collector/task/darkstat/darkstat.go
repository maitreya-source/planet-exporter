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

package darkstat

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"planet-exporter/collector/task/inventory"
	"planet-exporter/pkg/network"
	"planet-exporter/pkg/prometheus"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/prom2json"
	log "github.com/sirupsen/logrus"
)

// task that queries darkstat metrics and aggregates them into usable planet metrics
type task struct {
	enabled          bool
	darkstatAddr     string
	prometheusClient *prometheus.Client

	hosts []Metric
	mu    sync.Mutex
}

var (
	once      sync.Once
	singleton task
)

func init() {
	httpTransport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		ExpectContinueTimeout: 1 * time.Second,
	}

	singleton = task{
		enabled:          false,
		hosts:            []Metric{},
		mu:               sync.Mutex{},
		prometheusClient: prometheus.New(httpTransport),
	}
}

// InitTask initial states
func InitTask(ctx context.Context, enabled bool, darkstatAddr string) {
	once.Do(func() {
		singleton.enabled = enabled
		singleton.darkstatAddr = darkstatAddr
	})
}

// Metric contains values needed for planet metrics
type Metric struct {
	Direction       string // ingress or egress
	LocalHostgroup  string // e.g. hostgroup
	RemoteHostgroup string
	RemoteIPAddr    string
	LocalDomain     string // e.g. consul domain
	RemoteDomain    string
	Bandwidth       float64
}

// Get returns latest metrics from singleton
func Get() []Metric {
	singleton.mu.Lock()
	hosts := singleton.hosts
	singleton.mu.Unlock()

	return hosts
}

// Collect will process darkstats metrics locally and fill singleton with latest data
func Collect(ctx context.Context) error {
	if !singleton.enabled {
		return nil
	}

	if singleton.darkstatAddr == "" {
		return fmt.Errorf("Darkstat address is empty")
	}

	startTime := time.Now()

	ctxCollect, ctxCollectCancel := context.WithCancel(ctx)
	defer ctxCollectCancel()

	// Scrape darkstat prometheus endpoint for host_bytes_total
	var darkstatHostBytesTotalMetric *prom2json.Family
	darkstatScrape, err := singleton.prometheusClient.Scrape(ctxCollect, singleton.darkstatAddr)
	if err != nil {
		return err
	}
	for _, v := range darkstatScrape {
		if v.Name == "host_bytes_total" {
			darkstatHostBytesTotalMetric = v
			break
		}
	}
	if darkstatHostBytesTotalMetric == nil {
		return fmt.Errorf("Metric host_bytes_total doesn't exist")
	}

	// Extract relevant data out of host_bytes_total
	hosts, err := toHostMetrics(darkstatHostBytesTotalMetric)
	if err != nil {
		return err
	}

	singleton.mu.Lock()
	singleton.hosts = hosts
	singleton.mu.Unlock()

	log.Debugf("taskdarkstat.Collect retrieved %v downstreams metrics", len(hosts))
	log.Debugf("taskdarkstat.Collect process took %v", time.Since(startTime))
	return nil
}

// toHostMetrics converts darkstatHostBytesTotal metrics into planet explorer prometheus metrics.
func toHostMetrics(darkstatHostBytesTotal *prom2json.Family) ([]Metric, error) {
	var hosts []Metric

	inventoryHosts := inventory.Get()

	localAddr, err := network.DefaultLocalAddr()
	if err != nil {
		return nil, err
	}
	// To label source traffic that we need to build dependency graph
	localHostgroup := localAddr.String()
	localDomain := localAddr.String()
	localInventory, ok := inventoryHosts[localAddr.String()]
	if ok {
		localHostgroup = localInventory.Hostgroup
		localDomain = localInventory.Domain
	}
	log.Debugf("Local address don't exist in inventory: %v", localAddr.String())

	for _, m := range darkstatHostBytesTotal.Metrics {
		metric := m.(prom2json.Metric)

		ip := net.ParseIP(metric.Labels["ip"])

		// Skip its own IP as we don't need it
		if ip.Equal(nil) || ip.Equal(localAddr) {
			continue
		}

		inventoryHostInfo := inventoryHosts[metric.Labels["ip"]]

		bandwidth, err := strconv.ParseFloat(metric.Value, 64)
		if err != nil {
			log.Errorf("Failed to parse 'host_bytes_total' value: %v", err)
			continue
		}

		direction := ""
		// Reversed from netfilter perspective
		switch metric.Labels["dir"] {
		case "out":
			direction = "ingress"
		case "in":
			direction = "egress"
		}

		hosts = append(hosts, Metric{
			LocalHostgroup:  localHostgroup,
			RemoteHostgroup: inventoryHostInfo.Hostgroup,
			RemoteIPAddr:    metric.Labels["ip"],
			LocalDomain:     localDomain,
			RemoteDomain:    inventoryHostInfo.Domain,
			Direction:       direction,
			Bandwidth:       bandwidth,
		})
	}

	return hosts, nil
}
