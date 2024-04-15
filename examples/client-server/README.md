# Example defining and sending data through LoadBalancer with the HTTP Client

For this example all execution incorporated in `main.go`

Commands description available in:
```shell
go run main.go --help
```

## Step 1: Generate TLS data
It will generate full set of TLS certificates for Server and Client in the folder
```shell
go run main.go --gentls
```
## Step 2: Launch Servers behind LB
>   -routes {string} : Routes for the load balancer in the format 'localhost:9081,localhost:9082,localhost:9083'

```shell
 go run main.go --start-test-servers --routes localhost:9081,localhost:9082,localhost:9083
```
## Step 3: Launch LB
> -port {int} : Port for the load balancer service (default 9089)
> 
> -routes {string} : Routes for the load balancer in the format 'localhost:9081,localhost:9082,localhost:9083'
```shell
go run main.go --start-lb --routes localhost:9081,localhost:9082,localhost:9083
```
## Step 4: Use HTTP client script to send requests in parallel
> -threads {int} How many parallel clients should be there trying to reach the servers behind the LB (default 3)

```bash
go run main.go --send-client --threads=50
```
## Step 5: (optional) Remove TLS data
Will clean up the folder from TLS data
```shell
go run main.go --wipetls
```