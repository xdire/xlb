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

## Details

Design will discuss following areas:
- [General Structure](#structure)
- [Components](#components)
- [Rate Limiter](#rate-limiter)
- [Authentication and Credentials](#authentication-and-credential-provision)
- [Security](#security-authentication-and-credential-provision)

## Structure
Balancer provides following components in order of flow and appearance:

1) Rate Limiting System
    - Per Resource Token Bucket based Rate Limiter
    - Per LB — LRU TTL Rate Limiter
2) Resource Cache
    - map of customer pool records
3) Forwarder — backend entity providing management per customer pool
    - allows selection of the strategy (if supported more than single)
    - provides health check host tracker per pool
    - provides ability to add to the pool or remove from the pool
```
     ┌────────────────┐    ┌──────────┐   ┌───────────┐                       ┌──────────┐            ┌──────────┐
     │   IP LRU TTL   │    │ Resource │   │  Resource │                       │ Healthy  │            │ No error │
     │Rate Limit Check│    │  lookup  ├───▶   cache   │                       │  found   ├─────┐      │          │
     └────────────────┘    └─────▲────┘   └─────┬─────┘                       └────▲─────┘     │      └────▲─────┤
              ▲                  │              │                                  │           │           │     │
              │                  │              │                                  │           │           │     └────┐
              │                 ╱█╲             │                                 ╱█╲          │          ╱█╲         │
              └──────────┐     ╱███╲            │                                ╱███╲         │         ╱███╲        │
                         │    ╱█████╲           │                               ╱█████╲        │        ╱█████╲       │
                         │   ╱███████╲     ┌────▼────┐   ┌───────────┐         ╱███████╲       │       ╱███████╲      │
   ┌─────────┐  ┌─────┐  │  ╱█████████╲    │  Rate   │   │           │        ╱█████████╲      └──────▶█████████╲   ┌─▼───────┐
───▶ Request ├──▶ TLS │──┴─▶█HANDSHAKE█▏   │ limiter ├───▶ Forwarder ├───────▶█STRATEGY██▏           ▕█DIAL HOST█▏  │ Connect │
   └─────▲───┘  │ port│     ╲█████████╱    │  check  │   │           │        ╲█████████╱             ╲█████████╱   │ streams │
         │      └─────┘      ╲███████╱     └─────────┘   │           │         ╲███████▲       .─.     ╲███████╱    └─────────┘
         │                    ╲█████╱                    │  ┌─────┐  │    ┌───────████╱│ ┌────( 2 )     ╲█████╱
         │                     ╲███╱                     │┌─┤Cache├─┐│  ┌─┴──┐   ╲███╱ │ │ Ask `┬'       ╲███╱
         │                      ╲█╱                      ││ └─────┘ ◀┼──┤Peek│    ╲█╱  └─┤ next │         ╲█╱
         │                    ┌────┐                     ││┌───────┐││  └────┘     │     │ host │          │
         │                    │Fail│                     │││ Route │││             │     └┬─────┘          │
         │                    └──┬─┘                     ││└───────┘││             │      │      .─.       │
         │                       │                       ││┌───────┐││             │    ┌─┴─────( 1 ) ┌────▼─────┐
         │                       │                       │││ Route │││             │    │  Mark  `┬'  │   Error  │
         │                       │                       ││└───────┘◀┼─────────────┼────┤unhealthy◀───┤          │
         │                       ▼                       ││┌───────┐││             │    └─────────┘   └──────────┘
         │                ┌────────────┐                 │││ Route │││             │
         │                │ IP LRU TTL │                 ││└───────┘││             │
         │                │Rate Limiter│                 │└─────────┘│        ┌────▼─────┐
         │                │ Add Record │                 └───────────┘        │ Unhealthy│
         │                └────────────┘                                      │   only   │
    ┌────┴────┐                  │             ┌──────────────────────────────┴──────────┘
    │  Close  │◀─────────────────┼─────────────┘
    └─────────┘                  │
         ▲                       │
         │                       │
         └───────────────────────┘
```

## Components

### Configuration
LoadBalancer can be configured on new instance creation with following slice of Services
provided at the time of instance creation:

`{services: []ServicePool}` Where ServicePool is representing following interface:
```go
type ServicePool interface {
    // How each ServicePool identified, CN match
    Identity() string   
	// Port to listen for incoming traffic
    Port() int
	// Rate of times per time.Duration
    RateQuota() (int, time.Duration)
    // How many unauthorized attempts before IP cache placement
    UnathorizedAttempts() int
    // Bring host back in routable healthy state after this amount of validations
    HealthCheckValidations() int
    // Routes to route
    Routes() []Route
}

type Route interface {
	// Stores path of the upstream
	Path()   string
	// Provides information if route is active, in case of update
	// function can provide false and that will adjust behavior of forwarder
	Active() bool
}
```

As well load balancer will have method `AddServicePool(pool ServicePool)` to do following:
- if pool exists for identity, update this pool
- if pool did not exist, spawn all the required items and run routing

### Forwarder
```
                               ┌───────────────────┐
┌──────────────────────────────┤Forwarder Selector ├──────────────────────────────┐
│                              └───────────────────┘                              │
│                                  ┌───────────┐                                  │
│         ┌────────────────────────┤   Slice   ├────────────────────────┐         │
│         │                        └───────────┘                        │         │
│         │     ┌───────┐            ┌───────┐            ┌───────┐     │         │
│         │ ┌───┤*Route ├───┐    ┌───┤*Route ├───┐    ┌───┤*Route ├───┐ │         │
│         │ │   └───────┘   │    │   └───────┘   │    │   └───────┘   │ │         │
│         │ │┌─────────────┐│    │┌─────────────┐│    │┌─────────────┐│ │         │
│         │ ││   Address   ││    ││   Address   ││    ││   Address   ││ │         │
│         │ │└─────────────┘│    │└─────────────┘│    │└─────────────┘│ │         │
│         │ │┌─────────────┐│    │┌─────────────┐│    │┌─────────────┐│ │         │
│ ┌───────┼─▶│   Healthy   ││    ││   Healthy   ││    ││   Healthy   │◀─┼─────┐   │
│ │       │ │└─────────────┘│    │└─────────────┘│    │└─────────────┘│ │     │   │
│ │       │ │┌─────────────┐│    │┌─────────────┐│    │┌─────────────┐│ │     │   │
│ ├───────┼─▶│ Connections ││    ││ Connections ││    ││ Connections ││ │     │   │
│┌┴┐      │ │└─────────────┘│    │└─────────────┘│    │└─────────────┘│ │    ┌┴┐  │
││A│      │ │┌─────────────┐│    │┌─────────────┐│    │┌─────────────┐│ │    │A│  │
││T├──────┼─▶│   Active    ││    ││   Active    ││    ││   Active    │◀─┼────┤T│  │
││O│      │ │└─────────────┘│    │└─────────────┘│    │└─────────────┘│ │    │O│  │
││M│      │ └───────────────┘    └───────────────┘    └───────────────┘ │    │M│  │
││I│      │                                                             │    │I│  │
││C│      └─────────────────────────────▲───────────────────────────────┘    │C│  │
│└┬┘                              ┌─────┴──────┐                             └┬┘  │
│ │  ┌──────────────┐             │Mark !Active│          ┌────────────────┐  │   │
│ │  │              │             │ Append new │          │                │  │   │
│ └──│   Strategy   │             └─────┬──────┘       ┌─▶│ Health-watcher │──┘   │
│    │              │              ┌────┴─────┐        │  │                │      │
│    └──────────────┴────┐         │  Update  │        │  └────────────────┘      │
│                      ╔═╩════╦────▶   Mutex  │        │      ┌─────────┐         │
│                      ║ Each ║    └────▲─────┘        │      │   Add   │         │
│                      ║Next()║         │              └──────┤  Route  │         │
│                      ╚══════╝         │                     └─────────┘         │
└───────────────────────────────────────┼─────────────────────────────────────────┘
                                   ┌────┴────┐
                                   │ Update  │
                                   │ Routes  │
                                   └─────────┘ 
```
Forwarder provides the reference to the slice of `[]*Route`.

#### Property updates
Both `Strategy` and `Health-Watcher` can use the slice and use `atomics` to update
each `*Route` individually.

#### Pool updates
In case if `[]*Route` should change Forwarder will block on the `Update Mutex` adding to the
slice and marking entities that should be removed as inactive (as they still can be in reference
with the strategy which will block only on Next() entry).

Updates are rare, this design does not target additional optimizations.

#### Strategy Next()
Provided strategy will use the current reference for provide the selection
mechanics as a **forward iteration loop**.

Strategy will block `updateMutex` on `Next()` but in following way:
```go
func (s Strategy) Next() *Route {
	s.fwd.updateMutex.Lock()
	s.fwd.updateMutex.Unlock()
	// ... rest of the code
	// range s.fwd.Routes
}
```
This will adjust behavior to not pick the stale `[]*Route` if Update Routes started
to mark the entries and replacing the slice reference.

However, it will try to use the stale list once if it was fetched before Update Routes happened.

### Rate Limiter
Introducing 2 paths to enforce rate limiting per customer pool and for unauthorized
connections.
```
     ┌───────────┐
┌────┤Authorized ├────┐                         ┌───────────────┐
│    └───────────┘    │          ┌──────────────┤ Token Bucket  ├─────────────────┐
│                     │          │              └───────────────┘  ┌────────┐     │
│  ┌──────────────┐   │ ┌─────┐  │                              ┌──┤ Params ├──┐  │
│  │ Route Pool 1 │───┼─┤Has 1├──▶                              │  └────────┘  │  │
│  └──────────────┘   │ └─────┘  │  ┌─┐                         │CAP:10  TOK:10│  │
│                     │          │  │0│   ─┬─                   ├──────────────┤  │
│  ┌──────────────┐   │ ┌─────┐  │  │ │    │  ┌──┬──┬──┐        ├──────────────┤  │
│  │ Route Pool 2 │───┼─┤Has 1├──▶  │1│    │  │R1│R2│R3│        │-3      TOK:7 │  │
│  └──────────────┘   │ └─────┘  │  │ │    │  └──┴──┴──┘        ├──────────────┤  │
│                     │          │  │2│    │                    │ 0      TOK:7 │  │
│  ┌──────────────┐   │ ┌─────┐  │  │ │    │  ┌──┬──┬──┬──┐     ├──────────────┤  │
│  │ Route Pool N │───┼─┤Has 1├──▶  │3│    │  │R4│R5│R6│R7│     │+1 -4   TOK:3 │  │
│  └──────────────┘   │ └─────┘  │  │ │    │  ├──┼──┼──┼──┤ ┌──┐├──────────────┤  │
└─────────────────────┘          │  │4│ ┌─┐│  │R8│R9│R0│R1│ │XX││+1 -4   TOK:0 │  │
                                 │  │ │ │ ││  ├──┼──┼──┼──┘ └──┘├──────────────┤  │
                                 │  │5│ │ ││  │R2│XX│XX│        │+1 -1   TOK:0 │  │
                                 │  │ │ │T││  ├──┼──┼──┼──┐     ├──────────────┤  │
                                 │  │6│ │I││  │R3│XX│XX│XX│     │+1 -1   TOK:0 │  │
                                 │  │ │ │M││  ├──┼──┼──┼──┤     ├──────────────┤  │
                                 │  │7│ │E││  │R4│XX│XX│XX│     │+1 -1   TOK:0 │  │
                                 │  │ │ │ ││  └──┴──┴──┴──┘     ├──────────────┤  │
                                 │  │8│ │ ││                    │ 0      TOK:0 │  │
                                 │  │ │ └─┘│                    ├──────────────┤  │
                                 │  │9│    │                    │ 0      TOK:0 │  │
                                 │  │ │    │                    ├──────────────┤  │
                                 │  │0│   ─┼─                   │ 0      TOK:0 │  │
                                 │  │ │    │  ┌──┬──┬──┐        ├──────────────┤  │
                                 │  │1│    │  │R4│R5│R6│        │+4 -3   TOK:1 │  │
                                 │  │ │    │  ├──┼──┴──┘        ├──────────────┤  │
                                 │  │2│    │  │R7│              │+1 -1   TOK:1 │  │
                                 │  └─┘    │  └──┘              └──────────────┘  │
                                 │         ▼                                      │
                                 └────────────────────────────────────────────────┘



    ┌────────────┐
┌───┤Unauthorized├───┐              ┌──────────────┐
│   └────────────┘   │              │ Not In Cache │───┐
│                    │              └──────────────┘   │      ┌─────────────────┐
│      ┌──────┐      │                     Λ           │  ┌───┤  LRU+TTL CACHE  ├───┐
│      │ IP 1 │──────┼────┐               ╱ ╲          │  │   └─────────────────┘   │
│      └──────┘      │    │              ╱   ╲         └──┼▶──────┬────┬──────────┐ │
│      ┌──────┐      │    │             ╱     ╲           ││IPADDR│ 1  │*time.Time│ │
│      │ IP 2 │──────┼────┤            ╱       ╲          │├──────┼────┼──────────┤ │
│      └──────┘      │    ├───────────▶ Check   ▏         ││IPADDR│ 4  │*time.Time│ │
│      ┌──────┐      │    │            ╲ Cache ╱          │├──────┼────┼──────────┤ │
│      │ IP 3 │──────┼────┤             ╲     ╱           ││IPADDR│ 5  │*time.Time│ │
│      └──────┘      │    │              ╲   ╱            │└──────┴─▲──┴──────────┘ │
│      ┌──────┐      │    │               ╲ ╱             │    ┌────┘               │
│      │ IP N │──────┼────┘                V              └────┼────────────────────┘
│      └──────┘      │             ┌──────────────┐            │
│                    │             │   In Cache   │       ┌─────────┐
└────────────────────┘             └──────────────┘       │         │
                                           │              │Increment│
                                           │              │ counter │
                                           └─────────────▶│         │
                                                          │If < than│
                                                          │threshold│
                                                          │         │
                                                          └─────────┘
```
###### Per-session: When TLS Handshake happened and session got authorization
In this path we provide a Token bucket for each customer pool resource, which will
provide the sliding window for the authorized sessions.

For this design we assuming customer is using one balancer services pool as one instance of the
service provided and by that design implies rate limiter per one identity of customer pool.

###### Per IP address: When TLS Handshake failed 
As we must address possibly super-large chunk of IP data to store in memory, in the scope
of this library (not distributed) we will provide LRU TTL cache of size, which will update
and purge while being called for the records. 

1) Before TLS Handshake — we will peek in the cache to see if we should perform TLS at all
2) After TLS Handshake failed — we will put record in the cache

### Health Check Scheduler
For managing Unhealthy routes we can create simple service based on concurrent 
`Min-time-to-next-check` `PriorityQueue`. Forwarder can offer `*Route` to this scheduler.

- Scheduler will process `*Route` entries while `Active` and `!Healthy`.
- Scheduler can be configured to do multiple checks before promote to `Healthy` with `checks` param
- If `*Route` not `Active` anymore, it will not do `Push()` after `Pop()`

Design scheme represents all above properties nicely:
```
                                              ┌────────────────────────┐
  ┌───────────────────┐         ┌─────────────┤  Health Check Routine  ├────────────────┐
  │ SubmitUnhealthy() │         │             └────────────────────────┘                │
  └───────────────────┘         │                 ┌────────────────┐                    │
            │                   │                 │ Priority Queue │                    │
            │                   │                 └────────────────┘                    │
            │                   │                 ┌────────────────┐                    │
     ┌──────▼──────┐            │                 │     Poll()     │                    │
     │ ┌────────┐  │            │                 └───────┬────────┘                    │
     │ │ *Route │  │            │                         │                             │
     │ └────────┘  │            │                         ▼ ┌───────┐                   │
     │ ┌────────┐  │            │                        ╱ ╲│ Mutex │                   │
     │ │ checks │  │            │                       ╱   └───────┘    ┌─────────┐    │
     │ └────────┘  │            │  ┌───────┐           ┌─────┐           │priority │    │
     │ ┌────────┐  │            │  │ Empty ◀───────────│PEEK ├───────────▶    >    │    │
     │ │ passed │  │            │  └───┬───┘           └─────┘           │time.Now │    │
     │ └────────┘  │            │      │                ╲   ╱            └────┬────┘    │
     │ ┌────────┐  │            │      │                 ╲ ╱                  │         │
     │ │  next  │  │            │      │                  │                   │         │
     │ │ check  │  │            │┌─────▼─────┐            │            ┌──────▼──────┐  │
     │ └────────┘  │            ││   Await   │       ┌────▼────┐       │    Await    │  │
     └──────┬──────┘            ││           │       │time.Now │       │             │  │
        ┌───┴────┐              ││ <-newTask │       │    >    │       │  <-newTask  │  │
        │ Push() │              ││<-ctx.Done │       │ priority│       │<-time.After │  │
        └───┬────┘              ││           │       └────┬────┘       │ <-ctx.Done  │  │
            │                   ││sleeping=1 │            │            │             │  │
┌───────────▼──────────────┐    │└───────────┘       ┌────▼────┐       └─────────────┘  │
│ Priority Queue           │    │                    │  Pop()  │                        │
│                          │    │                    └────┬────┘                        │
│ MIN:(time.Now+nextCheck) │    │                         │                             │
└───────────┬──────────────┘    │                    ┌────▼────┐                        │
            │                   │             ┌──────│re Dial()│──────┐                 │
     ┌──────▼──────┐            │             │      └─────────┘      │                 │
     │             │            │             │                       ▼                 │
     │ if sleeping │            │             ▼                  ┌─────────┐            │
     │  newTask <- │            │        ┌────────┐              │ passed  │            │
     │  sleeping=0 │            │        │passed=0│              │   ==    │            │
     │             │            │        └────┬───┘              │ checks  │            │
     └─────────────┘            │             │                  └────┬────┘            │
                                │             │                       │                 │
                                │        ┌────▼───┐              ┌────▼────┐            │
                                │        │ Push() │              │ *Route  │            │
                                │        └────────┘              │ active  │            │
                                │                                └─────────┘            │
                                └───────────────────────────────────────────────────────┘
```

### Security Authentication and Credential provision

#### Client Service mTLS Layer
Proposed mTLS layer consists of the following scheme:
```
                                                                    ╔════════════╗
                                                      ┌─────────────╣    Load    ╠─────────────┐
                                                      │             ║  balancer  ║             │
                                                      │             ╚════════════╝             │
                                                      │                                        │
┌──────────┐                                          │       ┌───────────────┐                │
│          │                                       ┌──┼──────▶│  Accept Cert  │                │
│  CACert  │───────┐        ┌────────────┐         │  │       └───────┬───────┘                │
│          │       │        │            │         │  │               │                        │
└──────────┘  ┌────┴──┐     │   Client   │  ┌──────┴┐ │       ┌───────▼───────┐                │
              │Produce│     │    Cert    ├──┤Connect│ │       │  Fetch CN     │                │
┌──────────┐  │  TLS  ├────▶│            │  └───────┘ │       └───────────────┘                │
│  Client  │  └────┬──┘     │ CN=Identity│            │               │                        │
│ Frontend │       │        │            │            │       ┌───────┘                        │
│  Access  │───────┘        └────────────┘            │       │                                │
│    key   │                                         ┌┴───────▼───────┐                        │
└──────────┘                                         │ Lookup Access  │    ┌─────────────────┐ │
                                                     │  Key in Cache  │    │  Dispatch to    │ │
                                                     │  of available  │────▶correct Forwarder│ │
                                                     │ customer pools │    └─────────────────┘ │
                                                     └┬───────────────┘                        │
                                                      └────────────────────────────────────────┘
```
###### Common Name Identity
Provided to Load Balancer with `AddForwarderPool(pool ServicePool)` method where `ServicePool` is
the interface for the persistent configuration. `ServicePool.Identity()` will be used to ensure
`CN` match for authorization.

This design will limit scope of Identity to simple selection of the whole set of the customer pool
for such identity.

As the future next steps certificate can be enriched with property like the `OU=role` and this will
provide ability to have customer pools to routed for the authentication with additional precision.
###### Scope 
For the scope of the project and simplicity of initial implementation we consider following:
- Encryption Key RSA 3072
- Cipher Suites: TLS v1.3 compliant set
- Client Common Name will be pre-filled with Identity Key (for Forwarder selectors)
- TLS v1.3 as default configuration
###### Considerations
- mTLS Layer will match CN Access Key to the available Identity Keys in the connection manager
at the time of connection, scope should be limited to memory intensive operations for authorizing
the connection
- DDoS event might pile up the waiting connections on the port, raising opened descriptors on the 
Linux machine to the limits, after which we can lose replica. For this to happen flow of the connections
should exceed processing power for `certificate signature verification + hashMap access time`. 
- System should be able to drop connection and close descriptors as fast as possible
- Local IP Rate Limiter cache should help to drop unwanted connections for reasonable amount of IP Address
records, taking the LRU structure per IP at ~approx:
   - max(4 bytes (16 for str) + 1byte + Time(8 bytes) + DLL(8 + 8 + 8 bytes) + MAP(8 + 8 bytes)) < 128 bytes
   - in 1Kb we can carry at least 8 LRU records, 8000 in 1Mb or 800000 in 100Mb, 8M in 1Gb
   - capacity of 8M records to be dropped before additional CPU cycles can give some insurance
  
