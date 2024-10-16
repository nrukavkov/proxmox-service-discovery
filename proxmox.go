package main

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/go-resty/resty/v2"
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

// Structure for network interface data of the node
type NetworkInterface struct {
	Iface    string   `json:"iface"`
	Address  string   `json:"address"`
	Type     string   `json:"type"`
	Families []string `json:"families"`
}

type NodeNetworkResponse struct {
	Data []NetworkInterface `json:"data"`
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

// Function to get and update DNS records from Proxmox, including VMs and node network data
func updateRecordsFromProxmox(records map[string]string, proxmoxURL, apiToken, dnsSuffix, useProxmoxTags string) {
	// Temporary variable to hold the new records
	newRecords := map[string]string{}

	// Fetching all nodes
	var nodesResp ProxmoxNodesResponse
	err := fetchFromProxmox(fmt.Sprintf("%s/api2/json/nodes", proxmoxURL), apiToken, &nodesResp)
	if err != nil {
		log.Printf("Error fetching node list: %v. Skipping update.", err)
		return // Do not update records if an error occurs
	}

	// For each node, fetch VMs, their configuration, and network information
	for _, node := range nodesResp.Data {
		var vmsResp ProxmoxVMsResponse
		err := fetchFromProxmox(fmt.Sprintf("%s/api2/json/nodes/%s/qemu", proxmoxURL, node.Node), apiToken, &vmsResp)
		if err != nil {
			log.Printf("Error fetching VMs for node %s: %v. Skipping this node.", node.Node, err)
			continue // Skip this node and move to the next one
		}

		// For each VM, fetch configuration and extract IP address and name
		for _, vm := range vmsResp.Data {
			var configResp VMConfigResponse
			err := fetchFromProxmox(fmt.Sprintf("%s/api2/json/nodes/%s/qemu/%d/config", proxmoxURL, node.Node, vm.VMID), apiToken, &configResp)
			if err != nil {
				log.Printf("Error fetching configuration for VM %d on node %s: %v. Skipping this VM.", vm.VMID, node.Node, err)
				continue // Skip this VM and move to the next one
			}

			ip := extractIPFromConfig(configResp.Data.IPConfig0)
			if ip != "" {
				// Create DNS records based on the VM name
				if configResp.Data.Name != "" {
					newRecords[configResp.Data.Name+dnsSuffix] = ip
				}

				// Check if tags should be used to create DNS records
				if useProxmoxTags == "true" && vm.Tags != "" {
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

		// Fetch network information for the node
		var nodeNetworkResp NodeNetworkResponse
		err = fetchFromProxmox(fmt.Sprintf("%s/api2/json/nodes/%s/network", proxmoxURL, node.Node), apiToken, &nodeNetworkResp)
		if err != nil {
			log.Printf("Error fetching network information for node %s: %v. Skipping network records for this node.", node.Node, err)
			continue
		}

		// Add network information for interface vmbr0
		for _, iface := range nodeNetworkResp.Data {
			if iface.Iface == "vmbr0" && iface.Address != "" {
				// Add node's vmbr0 interface without adding 'vmbr0' to the DNS name
				newRecords[node.Node+dnsSuffix] = iface.Address
				break
			}
		}
	}

	// Update global records only if there were no errors
	for k, v := range newRecords {
		records[k] = v
	}

	log.Printf("Successfully updated records: %v", newRecords)
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
