const https = require("https");
const express = require("express");
const axios = require("axios");
const cors = require("cors");
const app = express();
app.use(cors());

app.use(express.json());

// GET route using Axios
app.post("/api/height", async (req, res) => {
  try {
    const url = req.body.node;
    const requestBody = {
      jsonrpc: "2.0",
      method: "zavax.getBlock",
      params: {},
      id: 1,
    };

    const response = await axios.post(url, requestBody, {
      httpsAgent: new https.Agent({ rejectUnauthorized: false }),
    });
    res.json(response.data);
  } catch (error) {
    res.status(500).json({ error: error });
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
    let response;
    for (let i = 0; i < 12; i++) {
      response = await getBlockHeight(req.body);
      if (response?.data?.error) break;
      if (response?.data?.result?.data?.height === req.body.block) {
        break;
      } else {
        await sleep(2000);
      }
    }
    res.json(response?.data ?? {});
  } catch (error) {
    res.status(500).json({ error: error });
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
