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

Design will discuss following areas:
- [General Structure](#structure)
- [High Level Components](#components)
- [Storage](#storage)
- [Authentication and Credentials](#authentication-and-credential-provision)
- [Security](#security)
- [Proto interfaces](#proto-specification)
- [Audit](#audit-events) and [Observability](#observability)
- [Product Usage](#product-usage)
- [Test plan](#test-plan)

### Structure
Balancer provides simple rule-based ingress.

There can be multiple variations of the component connectivity for the scope
of this project, we will touch Load balancer as an application available on a single
port, with the ability to route traffic to the provided server pool.

LB can be launched on as many ports as requested and have the same characteristics.
```
┌──────┐                 ┌───────┐                    ┌─────────────────┐
│      │                 │    B  │                    │  Customer pool  │
│Client│─────┐           │    A  │                    │  ┌──────────┐   │
│      │     │           │  L L  │      ┌─────────┬───┴──┤ Service  │   │
└──────┘  ┌──┴────▶─┬────┴┐ O A  │  ┌──▶│ .10.151 │ 9090 │ Instance │   │
┌──────┐  │ app1.d.c│ 8080│ A N  ├──┤   └─────────┴───┬──┴──────────┘   │
│      │  └──┬────▶─┴────┬┘ D C  │  │                 │  ┌──────────┐   │
│Client│─────┘           │    E  │  │   ┌─────────┬───┴──┤ Service  │   │
│      │                 │    R  │  ├──▶│ .10.152 │ 9090 │ Instance │   │
└──────┘                 └───────┘  │   └─────────┴───┬──┴──────────┘   │
                                    │                 │  ┌──────────┐   │
┌──────┐                 ┌───────┐  │   ┌─────────┬───┴──┤ Service  │   │
│      │  ┌─────────┬────┴┐   B  │  ├──▶│ .10.153 │ 9090 │ Instance │   │
│Client│──▶ app1.d.c│ 8080│   A  ├──┘   └─────────┴───┬──┴──────────┘   │
│      │  └─────────┴────┬┘ L L  │                    └─────────────────┘
└──────┘                 │  O A  │
┌──────┐                 │  A N  │
│      │  ┌─────────┬────┴┐ D C  │                    ┌─────────────────┐
│Client├─▶│ svc3.x.c│ 8081│   E  ├──┐                 │  Customer pool  │
│      │  └─────────┴────┬┘   R  │  │                 │  ┌──────────┐   │
└──────┘                 └───────┘  │   ┌─────────┬───┴──┤ Service  │   │
                                    ├──▶│ .72.91  │ 50051│ Instance │   │
┌──────┐                 ┌───────┐  │   └─────────┴───┬──┴──────────┘   │
│      │                 │    B  │  │                 │  ┌──────────┐   │
│Client│────┐            │    A  │  │   ┌─────────┬───┴──┤ Service  │   │
│      │    │            │  L L  │  │┌──▶ .72.92  │ 50051│ Instance │   │
└──────┘  ┌─┴───▶───┬────┴┐ O A  │  ││  └─────────┴───┬──┴──────────┘   │
┌──────┐  │ svc3.x.c│ 8081│ A N  │──┴┤                │  ┌──────────┐   │
│      │  └─┬───▶───┴────┬┘ D C  │   │  ┌─────────┬───┴──┤ Service  │   │
│Client│────┘            │    E  │   └──▶ .72.93  │ 50051│ Instance │   │
│      │                 │    R  │      └─────────┴───┬──┴──────────┘   │
└──────┘                 └───────┘                    └─────────────────┘
```

### Components

For our implementation, we will consider the following scheme:
```
                                                                                                                   
                ┌───────────────────────┐   ┌────┐              ┌─────────┐                                        
           ┌───▶│     API INTERFACE     │───┤CRUD├────┐    ┌────┤ options ├────┐                                   
         ┌─┴─┐  └───────────────────────┘   └────┘    │    │┌───┴─────────┴───┐│  ┌──────┐                         
         │ C │                                        ▼ ┌──□│addr1,addr2,addr3││  │      │            ┌─────────┐  
         │ O │                                     ┌────┴┬─│├─────────────────┤│──┘      ▼         ┌──┤Customer ├─┐
         │ N │                                     │  F  │ ││   timeoutSec    ││      ┌─────┐      │  │  Pool   │ │
         │ F │                                     │  R  │ │└─────────────────┘│      │  B  │      │  └─────────┘ │
         │ I │                                     │  O  │ └───────────────────┘      │  A  │      │              │
         │ G │       ┌─────────┐           ┌────┐  │  N  │                            │  C  │      │  ┌────────┐  │
         │ U │     ┌─┤Request 1├─┐      ┌──┤ R1 ├─▶│  T  │                            │  K  │────┬─┼─▶│  addr1 │  │
         │ R │     │ └─────────┘ │      │  └────┘  │  E  │                            │  E  │    │ │  └────────┘  │
         │ E │     │             │      │          │  N  │                            │  N  │    │ │  ┌────────┐  │
         └─┬─┘┌────────┐         ▼      │          │  D  ├────────────────────────────▶  D  │    ├─┼──▶  addr2 │  │
           │  │        │     ┌──────┬──────┐       └─────┘                            └─────┘    │ │  └────────┘  │
           └──│ Client │     │ 8090 │ mTLS │                                                     │ │  ┌────────┐  │
              │        │     └──────┴──────┘       ┌─────┐                            ┌─────┐    └─┼─▶│  addr3 │  │
              └────────┘         │      │          │  F  │                            │  B  │      │  └────────┘  │
                   │             │      │          │  R  ├────────────────────────────▶  A  │      │  ┌────────┐  │
                   │ ┌─────────┐ │      │          │  O  │                            │  C  │    ┌─┼─▶│  addr4 │  │
                   └─┤Request 2├─┘      │  ┌────┐  │  N  │                            │  K  │────┤ │  └────────┘  │
                     └─────────┘        └──┤ R2 ├─▶│  T  │      ┌─────────┐           │  E  │    │ │  ┌────────┐  │
                                           └────┘  │  E  │ ┌────┤ options ├────┐      │  N  │    └─┼─▶│  addrN │  │
                                                   │  N  │ │┌───┴─────────┴───┐│      │  D  │      │  └────────┘  │
                                                   │  D  │ ││addr4,addr5,addrN││      └─────┘      │              │
                                                   └────┬┴─│├─────────────────┤│──┐      ▲         └──────────────┘
                                                        └──□│   timeoutSec    ││  │      │                         
                                                           │└─────────────────┘│  └──────┘                         
                                                           └───────────────────┘                                   
```
#### Frontend
Is the part of the application customer can configure, and it represents Load Balancer
object options.

Frontend Available in Api Interface, and is entity in Storage Abstraction.

#### Backend
Is the internal abstraction created with Frontend options when the Connection Manager receives
the request. Backends have the following functionality:
- Provide a routine to pipe the traffic
- Apply routing strategy
- Manage connection health per pool
- Provide balancing with available algorithms

###### Balancing Algorithms
This design limits initial implementation to have the following algorithms:
1) Round Robin
   - implementation in a form of atomic counter to the size of the pool of servers
2) Least connection
   - implementation as a range over the pool of server records with atomically incremented counters

#### API Interface
API type of abstraction works with the database abstraction and can alter the Load Balancer
components behavior.

The following methods are to be proposed for the API Interface to fill the scope of this design:

---
`POST` `/api/v1/client` - creates client id and oauth type key

---
`POST` `/api/v1/client/auth` - authenticates client with **Basic** Authorization, gives back AccessToken

---
`POST` `/api/v1/frontend` - requires token and frontend object, creates frontend object 
and dispatches it to connection manager

---
`GET` `/api/v1/frontend/{uuid}/tls` - requires token and frontend UUID to provision the certificate authority
for the client devices

---
`PATCH` `/api/v1/frontend/{uuid}` - requires token, frontend UUID and frontend object with updated
properties, will update frontend and can remove it from being scheduled by connection manager

---
`GET` `/api/v1/frontend/{uuid}` - requires token, frontend UUID, provides back frontend object

---
`GET` `/api/v1/frontend/list` - requires token, gives back list of frontends which belongs to the client

---
`DELETE` `/api/v1/frontend/{uuid}` - requires token, will remove frontend from the dispatch and storage

---

### Storage
Provides persistence for the application entities using some storage backend, examples:
- `In Memory Backend`: suitable for testing application
- `Key-Val file storage`: suitable for running application on a single machine or for testing
- `Cloud database`: suitable for running multiple replicas of the application with centralized storage

Can be represented at minimal as following interface:
```go
type IStorageBackend interface {
	Init(ctx context.Context, opt IStorageOptions) error
	Close() error
	CreateClient(name string) (*entity.Client, error)
	GetClient(uuid string) (*entity.Client, error)
	CreateFrontend(opt *entity.Frontend) (*entity.Frontend, error)
	GetFrontend(uuid string) (*entity.Frontend, error)
	UpdateFrontend(uuid string, opt *entity.Frontend) error
	DeleteFrontend(uuid string) error
	CreateFrontendTLS(frontendUuid, clientUuid string) (*entity.FrontendTLSData, error)
	ListFrontend(clientUuid string, onlyActive bool) ([]*entity.Frontend, error)
}
```
Entity description can be found in [Proto](#proto-specification) section

### Authentication and credential provision
Authentication and provision flow can be represented as a following scheme:
```
                                                                                       
                                                       ┌─────────────────┬─────┬─────┐ 
            ┌─────────────────────────────────────────▶│ Create ClientID │     │     │ 
            │                                          └─────────────────┤     │     │ 
            │                                                            │     │     │ 
            │                     ┌─────────────────────┐                │     │     │ 
            │                     │ Receive Id and Key  ╠════════════════╣     │     │ 
      ┌──────────┐                └──────────╦──────────┘                │     │     │ 
      │          │◀══════════════════════════╝                           │     │     │ 
      │          │                                                       │     │     │ 
      │          │         ┌────────────┐              ┌─────────────────┤     │     │ 
      │          ├─────────┤Basic Id:Key├─────────────▶│ Get Access Token│  H  │  A  │ 
      │          │         └────────────┘              └─────────────────┤  T  │  P  │ 
      │          │                  ┌────────────────────┐               │  T  │  I  │ 
      │          ◀══════════════════╣ Receive JWT Token  │               │  P  │     │ 
      │          │                  └────────────────────╩═══════════════╣  S  │  I  │ 
      │          │                                                       │     │  N  │ 
      │          │         ┌────────────┐              ┌─────────────────┤  S  │  T  │ 
      │          ├─────────┤ Bearer JWT ├─────────────▶│ Create Frontend │  E  │  E  │ 
      │          │         └────────────┘              └─────────────────┤  R  │  R  │ 
      │          │                                                       │  V  │  F  │ 
      │ Customer ◀════════════╦─────────────────────────┐                │  E  │  A  │ 
      │          │            │ Receive Frontend Object ╠════════════════╣  R  │  C  │ 
      │          │            └─────────────────────────┘                │     │  E  │ 
      │          │                                                       │     │     │ 
      │          │         ┌────────────┐    ┌───────────────────────────┤     │     │ 
      │          ├─────────┤ Bearer JWT ├───▶│ Get Frontend Cert and Key │     │     │ 
      │          │         └────────────┘    └───────────────────────────┤     │     │ 
      │          │                                                       │     │     │ 
      │          │           ┌──────────────────────────────────┐        │     │     │ 
      │          ◀═══════════╣  Receive B64 Cert, Key, CACert   ╠════════╣     │     │ 
      │          │           └──────────────────────────────────┘        └─────┴─────┘ 
      │          │                                                                     
      │          │                                                                     
      │          │                                                                     
      └──────────┘                                                                     
            │                         ┌────────────┐                                   
            │                         │            │                                   
            │     ┌───────────┐       │  Customer  │       ┌─────┐       ┌────────────┐
            └─────┤Cert,Key,CA├──────▶│  Service   │───────┤ TLS ├──────▶│  TCP PORT  │
                  └───────────┘       │            │       └─────┘       └────────────┘
                                      └────────────┘                                   
```
### Security
Security for the application mainly presented as two sections:

#### Client API and Signed JWT AccessToken
###### Flow
- (out of scope) Client uses HTTPS Access to create account with SSO, flow should be 
provided by external application which then uses APIs mentioned in this design document
- Client uses HTTPS Access to provision ClientID and Key (Application) once per configuration
session
- Client operates JWT AccessToken for the rest of configuration session
###### Considerations
> `/api/v1/client/auth` can experience bruteforce attacks
- **Mitigation**: Auth API can be Throttled per IP basis to 10 rps, as this API 
should not provide capabilities for rapid traffic increases
- **Security**: Both ClientId and ClientKey should have enough entropy in a combination
- **Stolen credentials**: Client record can be disabled and this should trigger all the 
Frontend records belonging to the customer change their Access Key which will invalidate
selector capabilities to route the traffic for those records
- **DDoS**: As we scale balancer using the centralized storage, and balancer considered to be
ephemeral in context of multiple-replicas — we can lose replicas and create new replicas on the go

#### Client Service mTLS Layer
Proposed mTLS layer consists of the following scheme:
```
                                                                    ╔════════════╗
                                                      ┌─────────────╣ Connection ╠─────────────┐
                                                      │             ║  manager   ║             │
                                                      │             ╚════════════╝             │
                                                      │                                        │
┌──────────┐                                          │       ┌───────────────┐                │
│          │                                       ┌──┼──────▶│  Accept Cert  │                │
│  CACert  │───────┐        ┌────────────┐         │  │       └───────┬───────┘                │
│          │       │        │            │         │  │               │                        │
└──────────┘  ┌────┴──┐     │   Client   │  ┌──────┴┐ │       ┌───────▼───────┐                │
              │Produce│     │    Cert    ├──┤Connect│ │       │  Fetch CN     │                │
┌──────────┐  │  TLS  ├────▶│            │  └───────┘ │       └───────────────┘                │
│  Client  │  └────┬──┘     │ CN=FeAcKey │            │               │                        │
│ Frontend │       │        │            │            │       ┌───────┘                        │
│  Access  │───────┘        └────────────┘            │       │                                │
│    key   │                                         ┌┴───────▼───────┐                        │
└──────────┘                                         │ Lookup Access  │    ┌─────────────────┐ │
                                                     │  Key in Cache  │    │  Dispatch to    │ │
                                                     │  of available  │────▶ correct Backend │ │
                                                     │ customer pools │    └─────────────────┘ │
                                                     └┬───────────────┘                        │
                                                      └────────────────────────────────────────┘
```
###### Scope 
For the scope of the project and simplicity of initial implementation we consider following:
- Encryption Key RSA 3072
- Cipher Suites: TLS v1.3 compliant set
- Client Common Name will be pre-filled with Frontend Access Key (for active selectors)
- TLS v1.3 as default configuration
###### Considerations
- mTLS Layer will match CN Access Key to the available Access Keys in the connection manager
at the time of connection, scope should be limited to memory intensive operations for authorizing
the connection
- DDoS might pile up the waiting connections on the port, raising opened descriptors on the 
Linux machine to the limits, after which we can lose replica. For this to happen flow of the connections
should exceed processing power for `certificate signature verification + hashMap access time`. 
- System should be able to drop connection and close descriptors as fast as possible

### Privacy

No PII Considered in scope of this design

### UX

UX is not considered as a part of this design document, however API endpoints provided
for further UX/UI implementation

### Proto Specification

For simplicity of our application design and further integrations, common entities system uses
for operations in main components will be provided as generated `proto` structs, benefits and 
drawbacks of Protobuf as the schema design are not in the scope of this design document

##### Proto definitions
Minimal set to cover scope of this design
```protobuf
syntax = "proto3";
package github.com.xdire.xlb.v1;
import "google/protobuf/timestamp.proto";

enum Strategy {
  RoundRobin = 0;
  LeastConn  = 1;
}

// API to manage client data
message Client {
  string uuid = 1;
  string key  = 2;
  string name = 3;
  google.protobuf.Timestamp createdAt = 10;
}

// API to fetch the token
message AccessToken {
  string token = 1;
  int32  expiresIn = 2;
}

// API to fetch the TLS credentials
message FrontendTLSData {
  string key = 2;
  string certificate   = 3;
  string caCertificate = 4;
}

// API to manage Frontend information and status
message Frontend {
  string   uuid     = 1;
  bool     active   = 2;
  Strategy strategy = 3;
  int32    routeTimeoutMsec = 4;
  string   clientId  = 5;
  string   accessKey = 6;
  repeated FrontendRoute routes = 8;
}

message FrontendRoute {
  string route    = 1;
  int32  capacity = 3;
}

// If we provide API to see current backend snapshot
message Backend {
  string   uuid       = 1;
  string   frontendId = 2;
  Strategy strategy   = 3;
  repeated BackendRoute routes = 4;
}

message BackendRoute {
  string route       = 1;
  uint32 connections = 2;
}
```

### Audit Events
Audit events are not in the scope of initial implementation, but for the next iterations
can be provided in following components:
#### API Interface
Events:
- Client created
- Client token provision
- Client creates frontend
- Client alters frontend

#### Frontend component 
Events:
- Frontend accessed by client certificate with result of mTLS decision (Nsec batches)
- Frontend shutdown with reason

### Observability
Observability events are not in the scope of initial implementation, but for the next iterations
can be provided in the following components
#### Frontend component
###### Can contribute following metrics:
- Connection verified by mTLS for the Labels: FrontendId ClientId
- Connection rejected with reason for the Labels: FrontendId ClientId Cause[mTLS,Limiter]

###### Alerts:
- Connection rejected for Labels: FrontendId ClientId Cause[mTLS,NoActiveFrontend,Limiter]

#### Backend component
###### Can contribute following metrics:
- Backend accepted connection for the Labels: FrontendId ClientId ADDR-out
- Backend connection was rejected for the Labels: FrontendId ClientId ADDR-out Cause[Timeout,Rejected]
- Backend connection closed for the Labels: FrontendId ClientId ADDR-out | Values: connection time, bytes

###### Alerts:
- Backend connection rejection thresholds by ADDR-out
- Backend connection hitting the limits for provided thresholds in ADDR-out Route

### Product Usage

Product can be used as the containerized API application providing following capabilities:
- API Interface interacting with Centralized Database to scale up Frontends
- Container can receive events from Pub-Sub to activate/deactivate/invalidate certain resource and
change the behavior

### Test Plan

For the scope of this design tests will be designed in following areas:
- Tests for routing strategies to ensure proper operation capabilities and proper concurrency implementation
- Tests that components follow the context closure
- Functional tests with concurrency to ensure traffic flows without issues between clients and customer pool
