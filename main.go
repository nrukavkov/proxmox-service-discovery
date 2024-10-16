package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/joho/godotenv"
	"github.com/miekg/dns"
)

// Structure for the Proxmox API response for nodes
type Node struct {
	Node string `json:"node"`
}

// Structure for the Proxmox API response for VMs
type VM struct {
	VMID int    `json:"vmid"` // VMID is an integer in the API response
	Tags string `json:"tags"` // Tags separated by semicolons
}

// Structure for the Proxmox API response for VM configuration (contains nested "data" field)
type VMConfigResponse struct {
	Data VMConfig `json:"data"` // Nested object containing ipconfig0 and name
}

type VMConfig struct {
	IPConfig0 string `json:"ipconfig0"` // IP configuration
	Name      string `json:"name"`      // Name of the VM
}

// Example structure for the Proxmox API response
type ProxmoxNodesResponse struct {
	Data []Node `json:"data"`
}

type ProxmoxVMsResponse struct {
	Data []VM `json:"data"`
}

// Resty client for HTTP requests
var client = resty.New()

// DNS A-records will be updated every 60 seconds
var records = map[string]string{}

// Generic function for Proxmox API requests
func fetchFromProxmox(url, apiToken string, result interface{}) error {
	resp, err := client.R().
		SetHeader("Authorization", "PVEAPIToken="+apiToken).
		Get(url)

	if err != nil {
		return err
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
	}

	err = json.Unmarshal(resp.Body(), &result)
	if err != nil {
		return err
	}

	return nil
}

// Function to get and update DNS records from Proxmox
func updateRecordsFromProxmox() {
	// Reading environment variables
	proxmoxURL := os.Getenv("PROXMOX_URL")
	apiToken := os.Getenv("PVE_API_TOKEN")
	dnsSuffix := os.Getenv("DNS_SUFFIX") // Reading the DNS suffix

	// Checking for the presence of environment variables
	if proxmoxURL == "" || apiToken == "" {
		log.Fatal("Environment variables PROXMOX_URL and/or PVE_API_TOKEN are not set")
	}

	if dnsSuffix == "" {
		log.Fatal("Environment variable DNS_SUFFIX is not set")
	}

	// Fetching all nodes
	var nodesResp ProxmoxNodesResponse
	err := fetchFromProxmox(proxmoxURL+"/api2/json/nodes", apiToken, &nodesResp)
	if err != nil {
		log.Fatalf("Error fetching node list: %v", err)
	}

	// Updating records
	newRecords := map[string]string{}

	// For each node, fetch VMs and their configuration
	for _, node := range nodesResp.Data {
		var vmsResp ProxmoxVMsResponse
		err := fetchFromProxmox(proxmoxURL+"/api2/json/nodes/"+node.Node+"/qemu", apiToken, &vmsResp)
		if err != nil {
			log.Printf("Error fetching VMs for node %s: %v", node.Node, err)
			continue
		}

		// For each VM, fetch configuration and extract IP address and name
		for _, vm := range vmsResp.Data {
			var configResp VMConfigResponse
			err := fetchFromProxmox(proxmoxURL+"/api2/json/nodes/"+node.Node+"/qemu/"+fmt.Sprint(vm.VMID)+"/config", apiToken, &configResp)
			if err != nil {
				log.Printf("Error fetching configuration for VM %d on node %s: %v", vm.VMID, node.Node, err)
				continue
			}

			ip := extractIPFromConfig(configResp.Data.IPConfig0)
			if ip != "" {
				// Create DNS records based on the VM name
				if configResp.Data.Name != "" {
					newRecords[configResp.Data.Name+dnsSuffix] = ip
				}

				// Split tags by semicolons and create DNS records based on tags
				if vm.Tags != "" {
					tags := strings.Split(vm.Tags, ";")
					for _, tag := range tags {
						tag = strings.TrimSpace(tag) // Trim any extra spaces
						tag = sanitizeTag(tag)       // Additional tag sanitization
						if tag != "" {
							// Create a record based on the tag and IP address
							newRecords[tag+dnsSuffix] = ip
						}
					}
				}
			}
		}
	}

	// Update global records
	records = newRecords
	log.Printf("Updated records: %v", records)
}

// Function to extract IP address from the ipconfig0 string
func extractIPFromConfig(ipconfig string) string {
	// Example string: "ip=10.0.5.62/24,gw=10.0.5.1"
	re := regexp.MustCompile(`ip=([\d\.]+)`)
	matches := re.FindStringSubmatch(ipconfig)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// Additional function to sanitize tags
func sanitizeTag(tag string) string {
	// Add any logic to remove forbidden characters
	return strings.ToLower(tag) // For example, convert all tags to lowercase
}

// Function to handle DNS requests
func handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true

	found := false

	for _, q := range r.Question {
		switch q.Qtype {
		case dns.TypeA:
			if ip, ok := records[q.Name]; ok {
				rr := new(dns.A)
				rr.Hdr = dns.RR_Header{
					Name:   q.Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    60,
				}
				rr.A = net.ParseIP(ip)
				msg.Answer = append(msg.Answer, rr)
				found = true
			}
		}
	}

	if !found {
		c := new(dns.Client)
		externDNS := "8.8.8.8:53"
		in, _, err := c.Exchange(r, externDNS)
		if err != nil {
			log.Printf("Error during recursive query: %v", err)
			return
		}

		if in != nil {
			w.WriteMsg(in)
			return
		}
	}

	if err := w.WriteMsg(msg); err != nil {
		log.Printf("Error sending response: %v", err)
	}
}

func main() {
	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Periodically update records every 1 minute
	go func() {
		for {
			updateRecordsFromProxmox()
			time.Sleep(60 * time.Second) // 1 minute
		}
	}()

	dns.HandleFunc(".", handleDNSRequest)

	server := &dns.Server{Addr: ":2053", Net: "udp"}

	log.Printf("Starting DNS server on port 2053...")
	err = server.ListenAndServe()
	if err != nil {
		log.Fatalf("Failed to start DNS server: %v", err)
	}
}
