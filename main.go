package main

import (
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/miekg/dns"
)

var records = map[string]string{}

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

	// Read environment variables
	proxmoxURL := os.Getenv("PROXMOX_URL")
	apiToken := os.Getenv("PVE_API_TOKEN")
	dnsSuffix := os.Getenv("DNS_SUFFIX")
	useProxmoxTags := os.Getenv("DISCOVERY_VM_TAGS")
	discoveryCIDR := os.Getenv("DISCOVERY_NODE_CIDR")
	port := os.Getenv("DNS_LISTEN_PORT")
	if port == "" {
		port = "2053" // Default port
	}
	refreshSecondsStr := os.Getenv("DNS_REFRESH_SECONDS")
	refreshSeconds, err := strconv.Atoi(refreshSecondsStr)
	if err != nil || refreshSeconds <= 0 {
		refreshSeconds = 60 // Default to 60 seconds
	}

	// Periodically update records based on refresh interval
	go func() {
		for {
			updateRecordsFromProxmox(records, proxmoxURL, apiToken, dnsSuffix, useProxmoxTags, discoveryCIDR)
			time.Sleep(time.Duration(refreshSeconds) * time.Second)
		}
	}()

	dns.HandleFunc(".", handleDNSRequest)

	server := &dns.Server{Addr: ":" + port, Net: "udp"}

	log.Printf("Starting DNS server on port %s...", port)
	err = server.ListenAndServe()
	if err != nil {
		log.Fatalf("Failed to start DNS server: %v", err)
	}
}
