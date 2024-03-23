# ZavaX Virtual Machine

Avalanche is a network composed of multiple subnets, and within each subnet, blockchains. Each blockchain is an instance of a [Virtual Machine (VM)](https://docs.avax.network/learn/platform-overview#virtual-machines), much like an object in an object-oriented language is an instance of a class. That is, the VM defines the behavior of the blockchain.

ZavaX VM defines a blockchain that provides oracle services from the Zcash blockchain. Each block in the blockchain contains the Zcash block. This VM demonstrates capabilities of custom VMs and custom blockchains. For more information, see: [Create a Virtual Machine](https://docs.avax.network/build/tutorials/platform/create-a-virtual-machine-vm).

## Running the VM
[`scripts/run.sh`](scripts/run.sh) automatically installs [avalanchego], sets up a local network,
and creates a `zavax` genesis file.

*Note: The above script relies on ginkgo to run successfully. Ensure that $GOPATH/bin is part of your $PATH before running the script.*  

```bash
# to startup a local cluster (good for development)
cd ${HOME}/go/src/github.com/red-dev-inc/zavax-oracle
./scripts/run.sh 1.11.2

# to run full e2e tests and shut down cluster afterwards
cd ${HOME}/go/src/github.com/red-dev-inc/zavax-oracle
E2E=true ./scripts/run.sh 1.11.2

# inspect cluster endpoints when ready
cat /tmp/avalanchego-v1.11.2/output.yaml
<<COMMENT
endpoint: /ext/bc/2VCAhX6vE3UnXC6s1CBPE6jJ4c4cHWMfPgCptuWS59pQ9vbeLM
logsDir: ...
pid: 12811
uris:
- http://127.0.0.1:9650
- http://127.0.0.1:9652
- http://127.0.0.1:9654
- http://127.0.0.1:9656
- http://127.0.0.1:9658
network-runner RPC server is running on PID 66810...

use the following command to terminate:

pkill -P 66810 && kill -2 66810 && pkill -9 -f vu3xjfNfwJcNq1c4yFzvjF2hz6t2HZ4uHaWWQJvo27oyF6czX

# Note that it is possible to run avalanchego with or without HTTPS support. These curl command examples 
# are for when HTTPS support is not enabled. 

# query zavax block and add to subnet if a block not exists
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "zavax.getBlockByHeight",
    "params":{
        "id":"123123"
    },
    "id": 1
}' -H 'content-type:application/json;' http://127.0.0.1:9652/ext/bc/2W3Gn3E3xKSeHQZP47iybpgH6pk3JRWbNQs9P2FrKvXcHSNteB
<<COMMENT
{"jsonrpc":"2.0","result": {"timestamp":"1668475950","data": {
    "hash": "0000000023402630cf9f54f7499cac4f6a57c3b37692ce174df44d3a1a979770",
    "confirmations": 2031187,
    "size": 11277,
    "height": 123123,
    ....
    ....
},"height":"1","id":"2RbyqtZcr8DWnxWjD2jLaPUsjd2cxMFbjz1kmJjR7gDpp3txvz","parentID":"SdVstz8FpkYxsneD2XQDk2CK7d1EBe4YVqkhftgbvUiyFfeHJ"},"id":1}
COMMENT

# view last accepted block
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "zavax.getBlock",
    "params":{},
    "id": 1
}' -H 'content-type:application/json;' http://127.0.0.1:9652/ext/bc/2W3Gn3E3xKSeHQZP47iybpgH6pk3JRWbNQs9P2FrKvXcHSNteB
<<COMMENT
{"jsonrpc":"2.0","result":{"timestamp":"1668475950","data": {
    "hash": "0000000023402630cf9f54f7499cac4f6a57c3b37692ce174df44d3a1a979770",
    "confirmations": 2031187,
    "size": 11277,
    "height": 123123,
    ....
    ....
},"height":"1","id":"2RbyqtZcr8DWnxWjD2jLaPUsjd2cxMFbjz1kmJjR7gDpp3txvz","parentID":"SdVstz8FpkYxsneD2XQDk2CK7d1EBe4YVqkhftgbvUiyFfeHJ"},"id":1}
COMMENT

# terminate cluster
pkill -P 66810 && kill -2 66810 && pkill -9 -f vu3xjfNfwJcNq1c4yFzvjF2hz6t2HZ4uHaWWQJvo27oyF6czX
```


> **Note:** Test module wasn't completely done with respect to ZavaX. Please ignore/skip any testing within the test module [tests](tests)