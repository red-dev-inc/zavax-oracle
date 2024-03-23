const https = require("https");
const express = require("express");
const axios = require("axios");
const cors = require("cors");
const app = express();
app.use(cors());

app.use(express.json());

const getBlock = async (req) => {
  const { node, lastBlockId } = req.body;
  const params = lastBlockId ? { id: lastBlockId} : {}
  const requestBody = {
    jsonrpc: "2.0",
    method: "zavax.getBlock",
    params,
    id: 1,
  };

  const response = await axios.post(node, requestBody, {
    httpsAgent: new https.Agent({ rejectUnauthorized: false }),
  });
  return response;
};
// GET route using Axios
app.post("/api/height", async (req, res) => {
  try {
    const response = await getBlock(req)
    res.json(response.data);
  } catch (error) {
    console.error(error)
    res.status(500).json({ error: "connection timeout" });
  }
});

const getBlockHeight = async (req) => {
  const { node, block } = req;
  const url = node;
  const requestBody = {
    jsonrpc: "2.0",
    method: "zavax.getBlockByHeight",
    params: {
      id: block,
    },
    id: 1,
  };

  const response = await axios.post(url, requestBody, {
    httpsAgent: new https.Agent({ rejectUnauthorized: false }),
  });
  return response;
};

const sleep = (ms) => {
  return new Promise((resolve) => setTimeout(resolve, ms));
};

app.post("/api/block", async (req, res) => {
  try {
    let response = await getBlockHeight(req.body);
    if (response?.data?.error || response?.data?.result?.data?.height === req.body.block) {
      res.json(response?.data ?? {});
      return
    } 
    const block = await getBlock(req);
    const { data: { result:{ height: lastBlockHeight } } } = block;
    
    for (let i = 0; i < 12; i++) {
      response = await getBlock(req)
      if (response?.data?.error) break;
      if (response?.data?.result?.data?.height === req.body.block) {
        break;
      } else {
        if(response?.data?.result?.height > lastBlockHeight){
          if (response?.data?.result?.data?.height === req.body.block) {
            break;
          }else{            
            req.lastBlockId =  response?.data?.result?.parentID || null
          }
        } else{
          req.lastBlockId = null
        }
        await sleep(2000);        
      }
    }
    res.json(response?.data ?? {});
  } catch (error) {
    console.error(error)
    res.status(500).json({ error: "connection timeout" });
  }
});

const verifyBlocks = async (req) => {
  const { node } = req;
  const url = node;
  const requestBody = {
    jsonrpc: "2.0",
    method: "zavax.reconcileBlocks",
    params: {},
    id: 1,
  };

  const response = await axios.post(url, requestBody, {
    httpsAgent: new https.Agent({ rejectUnauthorized: false }),
  });
  return response;
};

app.post("/api/verify", async (req, res) => {
  try {
    let response;
    response = await verifyBlocks(req.body);
    res.json(response?.data?.result?.height ?? []);
  } catch (error) {
    console.error(error)
    res.status(500).json({ error: "connection timeout" });
  }
});

app.use(express.static("./build", { lastModified: false, etag: false }));

/** All other routes redirected to front-end app */
app.get("*", function (req, res) {
  res.sendFile("index.html", {
    root: "./build",
    lastModified: false,
    etag: false,
  });
});

// Start the server
const port = 80;
app.listen(port, () => {
  console.log(`Server running on port ${port}`);
});
