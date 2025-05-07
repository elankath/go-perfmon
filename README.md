# go-perfmon
Go CLI Tool that monitors when named processes come live and charts cpu/mem until they die and produces report

## Usage
`go run main.go <flags> <list of processnames to monitor>`

Example: `go run main.go -interval 30s kube-apiserver etcd`

Flags:
```
  -errt int
        Probe error threshold beyond which proc monitoring will stop (default 3)
  -interval value
        monitoring interval (default 10s)
  -rd string
        Directory for reports and charts (default "/tmp")
  -wait
        Whether to wait until processes are available for monitoring or not (default true)
```