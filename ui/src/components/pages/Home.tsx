import React, { useState } from "react";
import { FaSpinner } from 'react-icons/fa';
// UI
import Label from "../atoms/Label";
import Selector from "../atoms/Selector";
import TextField from "../atoms/TextField";
import TextArea from "../atoms/TextArea";
import Button from "../atoms/Button";
import Nodes from "../../utils/constants";

const Home: React.FC = () => {
                                                                                                                                      
  const [curlScript, setCurlScript] = useState('')
  const [selectedNode, setSelectedNode] = useState('');
  const [queryBlock, setQueryBlock] = useState('')
  const [queryResponse, setQueryResponse] = useState('')
  const [latestBlock, setLatestBlock] = useState('')
  const [isLoading, setIsLoading] = useState(false);

  const queryZavaXBlockHeight = () => {

    fetch("/api/height",
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          node: selectedNode
        }),
      }
    )
      .then((response) => response.json())
      .then((data) => {
        setLatestBlock(data?.result?.height ?? null)
      })
      .catch((error) => {
        // Handle any errors here
        console.error((error))
      });

  }


  const queryZcashBlock = () => {
    reset()
    setIsLoading(true);
    setCurlScript(`curl -X POST --data '{ "jsonrpc": "2.0", "method": "zcash.getBlockByHeight", "params": { "id": ${Number(queryBlock)} }, "id": 1 }'   -H 'content-type:application/json;' ${selectedNode}`)
    fetch('/api/block',
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          node: selectedNode,
          block: Number(queryBlock),
        }),
      }
    )
      .then((response) => response.json())
      .then((data) => {
        // Handle the response data here        
        setIsLoading(false);
        setQueryResponse(JSON.stringify(data, null, 2))
        queryZavaXBlockHeight()
      })
      .catch((error) => {
        // Handle any errors here
        
        setIsLoading(false);
        setQueryResponse((error))
      });

  }

  const reset = () => {
    setCurlScript("")
    setQueryResponse("")
    setLatestBlock("")
  }

  const queryBlockHeight = () => {
    if (queryResponse) {
      const obj = JSON.parse(queryResponse);
      return obj?.result?.height ?? ""
    }
    return ""
  }

  return (
    <div>
      <Label
        className={`mainDescription mb-1`}
        text={`Use the ZavaX Avalanche Subnet to query the Zcash blockchain.`}
      />
      <Label
        className={`mainDescription mb-4`}
        text={`Pick which node you want to use to initiate the request and the block height of the block you want to see.`}
      />
      <div className={`form-padding mb-4`}>
        <Selector
          nodes={Nodes}
          name={`node`}
          className={`form-select color-borders col-2 custon-input-size custom-color-text-selector`}
          onChange={(event) => {
            const node = event.target.value; // Extract the selected value
            setSelectedNode(node)
            reset()
          }}
        />
        <TextField
          id={`block`}
          name={`block`}
          className={`blockInput form-control color-borders custon-input-size`}
          placeholder={`Block Height to query`}
          onChange={(event) => {
            // Handle the selected node change logic here
            const queryBlock = event.target.value;
            setQueryBlock(queryBlock)
            reset()
          }}
        />
        <Button
          id="query-zcash"
          text={`Submit to ZavaX`}
          className={`custom-submit-button btn btn-danger`}
          onClick={queryZcashBlock}
          disabled={!(queryBlock && selectedNode)}
        />

      </div>

      {/* REQUEST */}
      <div className="mb-5" style={{ padding: 0 }}>
        <Label className={`mainLabel mb-3`} text={`Request: `} />
        <TextArea
          className={`color-borders custom-textarea col-sm-12`}
          rows={6}
          content={curlScript}
        />
      </div>

      {/* RESULTS */}
      <div className="mb-3 textarea-container" style={{ padding: 0 }}>
        <Label className={`mainLabel mb-3`} text={`Response: `} />
        <TextArea
          className={`color-borders custom-textarea col-sm-12`}
          rows={12}
          content={queryResponse}
        />   
        {isLoading && (
        <div className="loader-overlay">
          <FaSpinner className="loader" />
        </div>
      )}     
      </div>

      <div className="mb-5" style={{ padding: 0 }}>
        <Label className={`mainDescription block-data mb-2`} text={`Data retrieved from ZavaX Subnet block: ${queryBlockHeight()}`} />
        <Label className={`mainDescription block-data mb-2`} text={`New ZavaX Oracle Subnet block height: ${latestBlock}`} />
      </div>
    </div>
  );
};

export default Home;
