import React, { useState } from "react";
import { FaSpinner } from 'react-icons/fa';
// UI
import Label from "../atoms/Label";
import Selector from "../atoms/Selector";
import TextField from "../atoms/TextField";
import TextArea from "../atoms/TextArea";
import Button from "../atoms/Button";
import Nodes from "../../utils/constants";
import Dialog from "../molecules/Dialog/Dialog";

const Home: React.FC = () => {

  
  const controller = new AbortController();
  const signal = controller.signal;                                                                                                                                      
  const [curlScript, setCurlScript] = useState('')
  const [selectedNode, setSelectedNode] = useState('');
  const [queryBlock, setQueryBlock] = useState('')
  const [queryResponse, setQueryResponse] = useState('')
  const [latestBlock, setLatestBlock] = useState('')
  const [isLoading, setIsLoading] = useState(false);
  const [verifyModaltext, updateVerifyModalText] = useState('')
  const [openModal, openVerifyModal] = useState(false);
  const [loading, openVerifyModalLoading] = useState(true);

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
        setLatestBlock(data?.result?.height ?? "")
      })
      .catch((error) => {
        // Handle any errors here
        console.error((error))
      });

  }

  const validateZcashBlock = async () => {
    openVerifyModalLoading(true);
    updateVerifyModalText('Rechecking')
    openVerifyModal(true);
    fetch('/api/verify', 
      {
        signal,
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
      if(data?.length === 0){
        updateVerifyModalText('All blocks have been verified and exactly match.');
        openVerifyModalLoading(false);      
      }else {
        updateVerifyModalText(`All blocks have been verified. Below are mismatched blocks ${data.join(",")}.`);
        openVerifyModalLoading(false);
      
      }      
    })
    .catch((error) => {
      // Handle any errors here
      updateVerifyModalText(JSON.stringify(error));
      openVerifyModalLoading(false);
      console.error((error))
    });
  }


  const queryZcashBlock = () => {
    reset()
    setIsLoading(true);
    setCurlScript(`curl --cacert ./certs/zavax-oracle-cert.pem -X POST --data '{ "jsonrpc": "2.0", "method": "zavax.getBlockByHeight", "params": { "id": ${Number(queryBlock)} }, "id": 1 }'   -H 'content-type:application/json;' ${selectedNode}`)
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
          id="query-zavax"
          text={`Submit to ZavaX`}
          className={`custom-submit-button btn btn-danger mb-5`}
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
          readonly={true}
        />
      </div>

      {/* RESULTS */}
      <div className="mb-3 textarea-container" style={{ padding: 0 }}>
        <Label className={`mainLabel mb-3`} text={`Response: `} />
        <TextArea
          className={`color-borders custom-textarea col-sm-12`}
          rows={12}
          content={queryResponse}
          readonly={true}
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
      <Button
          id="recheck-all-blocks"
          text={`Recheck All Blocks`}
          className={`custom-submit-button btn btn-danger mb-4`}
          onClick={validateZcashBlock}
          disabled={openModal || !selectedNode}
        />
      <Dialog loading={loading} content={verifyModaltext} show={openModal} handleClose={() => { controller.abort(); openVerifyModal(false); }} />
    </div>
  );
};

export default Home;
