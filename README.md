# procmon
Go CLI Tool and backing library that monitors when named processes specified as arguments come live and tracks cpu/mem until they die and produces report charts into a targer directory.

## Setup

`go install github.com/elankath/procmon`


## Usage
`procmon <flags> <list of processnames to monitor>`

Example: `procmon -n <reportNamePrefix> -interval 30s kube-apiserver etcd`

Flags:
```  
  -n string
    	report name prefix
  -d string
        Directory for reports and charts (default "/tmp")
  -errt int
        Probe error threshold beyond which proc monitoring will stop (default 3)
  -interval value
        monitoring interval (default 10s)
  -wait
        Whether to wait until processes are available for monitoring or not (default true)
```
