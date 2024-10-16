# Simple DNS-Based Proxmox Service Discovery

This project implements a simple DNS-based service discovery mechanism using Go for Proxmox Cluster. The service retrieves virtual machine (VM) information from a Proxmox API and automatically generates DNS `A` records based on VM names and tags. These records are updated periodically and can be used to discover services in a dynamic environment.

## Features

- Retrieves VM information (name, tags, IP) from the Proxmox API.
- Dynamically generates DNS `A` records based on VM names and tags.
- Updates DNS records every minute, ensuring the latest VM state.
- Uses a custom DNS suffix for generated records.
- Supports service discovery by allowing other services to resolve VMs by name or tag using DNS.

## How It Works

1. The service queries the Proxmox API to fetch a list of VMs for each node.
2. For each VM, the service retrieves the VM's configuration, including:
   - IP address (`ipconfig0`).
   - VM name (`name`).
   - VM tags (`tags`), which are separated by semicolons.
3. The service creates DNS `A` records for each VM based on its name and tags, appending a configurable DNS suffix to each record.
4. These records are updated in memory every 60 seconds to reflect changes in the Proxmox environment.
5. The service runs a DNS server on port `2053` that resolves DNS queries based on the stored records.

## Requirements

- Go 1.18+
- A running Proxmox instance with API access

## Installation

1. Clone the repository:

    ```bash
    git clone https://github.com/nrukavkov/proxmox-service-discovery.git
    cd proxmox-service-discovery
    ```
2. Install dependencies:

    ```bash
    go mod tidy
    ```
3. Create a .env file to configure the service:

    ```bash
    touch .env
    ```

4. In the .env file, add the following environment variables:

    ```
    PROXMOX_URL=https://your-proxmox-api-url
    PVE_API_TOKEN=your-proxmox-api-token
    DNS_SUFFIX=.example.com
    DNS_LISTEN_PORT=53 # use 2053 for local testing
    DNS_REFRESH_SECONDS=60 # how ofter go to proxmox api
    USE_PROXMOX_TAGS=true # if true proxmox-service-discovery records will be filled also with tags
    ```

5. Build and run the Go application:

    ```bash
    go build -o proxmox-service-discovery
    ./proxmox-service-discovery
    ```

The DNS server will start on port 2053 and will update DNS records every minute.

To test the DNS queries, you can use dig or any other DNS client:

    ```bash
    dig @localhost -p 2053 vm-name.example.com
    ```

Change DNS Port
The DNS server listens on port 2053 by default. You can change this by modifying the Addr field in the DNS server configuration:

    ```go
    server := &dns.Server{Addr: ":2053", Net: "udp"}
    ```

## License

This project is licensed under the MIT License. See the LICENSE file for details.