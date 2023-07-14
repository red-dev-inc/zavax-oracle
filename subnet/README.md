# ZavaX Virtual Machine

Avalanche is a network composed of multiple blockchains. Each blockchain is an instance of a [Virtual Machine (VM)](https://docs.avax.network/learn/platform-overview#virtual-machines), much like an object in an object-oriented language is an instance of a class. That is, the VM defines the behavior of the blockchain.

ZavaX defines a blockchain that is a zcash-avax server. Each block in the blockchain contains the zcash block. This VM demonstrates capabilities of custom VMs and custom blockchains. For more information, see: [Create a Virtual Machine](https://docs.avax.network/build/tutorials/platform/create-a-virtual-machine-vm)

## Running the VM
[`scripts/run.sh`](scripts/run.sh) automatically installs [avalanchego], sets up a local network,
and creates a `zcash` genesis file.

*Note: The above script relies on ginkgo to run successfully. Ensure that $GOPATH/bin is part of your $PATH before running the script.*  

```bash
# to startup a local cluster (good for development)
cd ${HOME}/go/src/github.com/red-dev-inc/zcash-oracle
./scripts/run.sh 1.10.2

# to run full e2e tests and shut down cluster afterwards
cd ${HOME}/go/src/github.com/red-dev-inc/zcash-oracle
E2E=true ./scripts/run.sh 1.10.2

# inspect cluster endpoints when ready
cat /tmp/avalanchego-v1.10.2/output.yaml
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

pkill -P 66810 && kill -2 66810 && pkill -9 -f vuF4cW6EQEknhDU36Q976iZoNWNkkbDEM87jasYF5JrCdUJan

# query zcash block and add to subnet if a block not exists
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "zcash.getBlockByHeight",
    "params":{
        "id":"123123"
    },
    "id": 1
}' -H 'content-type:application/json;' http://127.0.0.1:9652/ext/bc/2W3Gn3E3xKSeHQZP47iybpgH6pk3JRWbNQs9P2FrKvXcHSNteB
<<COMMENT
{"jsonrpc":"2.0","result": {
    "hash": "0000000023402630cf9f54f7499cac4f6a57c3b37692ce174df44d3a1a979770",
    "confirmations": 2031187,
    "size": 11277,
    "height": 123123,
    ....
    ....
},"id":1}
COMMENT

# view last accepted block
curl -X POST --data '{
    "jsonrpc": "2.0",
    "method": "zcash.getBlock",
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
pkill -P 66810 && kill -2 66810 && pkill -9 -f vuF4cW6EQEknhDU36Q976iZoNWNkkbDEM87jasYF5JrCdUJan
```
