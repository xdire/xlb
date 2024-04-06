---
authors: Anton Repin (github.com/xdire)
state: wip
---

# RFD 1 - General Design

## What

This RFD Details the aspects of implementation of TCP load balancer
up the following minimal requirements:

### Library
- A least connections forwarder implementation that tracks the number of connections per upstream.
- A per-client connection rate limiter implementation that tracks the number of client connections.
- Health checking for unhealthy upstreams. Should not be eligible to receive connections until upstream determined to be healthy.
### Server
- mTLS authentication to have the server verify identity of the client and client of the server
- A simple authorization scheme that defines what upstreams are available to which clients
- Accept and forward connections to upstreams using library

## Why

TCP Proxy Load balancer with mTLS can solve the following set of problems:
- Distribute the load for the domain record using the ruleset enforced by the customer
- Serve as a secure communications Gateway pass-through for the network of services
- Provide access management per client resource scope
- Provide ingress statistics and observability for customer network

## Details