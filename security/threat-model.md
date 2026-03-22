# ZavaX Oracle Threat Model

This threat model is based on the [Invariant-Centric Threat Modeling](https://github.com/defuse/ictm) methodology which focuses the discussion on what remains true about the integrity of the system under various threat scenarios.

**ZavaX Oracle** is a three-node permissioned (proof-of-authority) L1 blockchain that runs on the Avalanche Fuji Test Network. It is a proof-of-concept for **red·bridge oracle** and **red·bridge** (previously called ZavaX Bridge). As such, some findings here can be applied toward improving the security of **red·bridge**, and ultimately, that is the purpose of this analysis. However, this threat model only addresses threats to ZavaX Oracle, not **red·bridge**.


## Target Audience

The target audience for this threat model is software security auditors and others who think about blockchain and distributed systems security.

## Participants and Adversaries

Before describing the ZavaX Oracle itself in detail, let's first define a number of *Participants*, an *Asset Registry*, *Security Properties* of concern, and *Adversaries* that will help to illustrate how ZavaX Oracle functions and various attack scenarios.

### Participants

Participant **Red** created the ZavaX Oracle permissioned (proof-of-authority) L1 and, as the *authority* for the L1, holds private keys that allow the addition and removal of nodes and other parameter changes. **Red** has hand-picked **Alice**, **Bob**, and **Carol** as node operators. **Red** also runs continuous reconciliation across Alice's, Bob's, and Carol's RPC outputs and checks disputed results against Zcash ground-truth data in order to detect operator or infrastructure compromise.

Participants **Alice**, **Bob**, and **Carol** each controls one ZavaX Oracle node and an associated RPC proxy server. **Alice** owns node 1, **Bob** owns node 2, and **Carol** owns node 3.

**Oscar** would like to use the ZavaX Oracle platform to find out the contents of a Zcash block at a particular block height. As such, Oscar represents the typical end-user of ZavaX Oracle. So, in addition to Red's monitoring, **Oscar** may independently notice anomalous responses and report them to Red and node operators.

**Walter** operates the web server that publicly hosts the ZavaX Oracle UI. **Xavier** operates an unpublished web server that can be used in place of Walter's in times of attack by adversaries. **Alice** and **Bob** only allow RPC requests from Walter's and Xavier's web servers. **Carol** allows RPC requests from anywhere on the Internet via HTTPS, for instance by **Oscar** using the Linux *curl* command on his home computer.

**The ZavaX Oracle Community** includes everyone who uses the ZavaX Oracle blockchain for any purpose, including all of the above participants.

### Asset Registry

Now let's look at the parts of the **ZavaX Oracle** blockchain. This registry defines assets to be secured and a baseline set of trust attributes used throughout attack scenarios. (A *Platform Diagram* showing how assets A-02 through A-09 connect with each other can be found later in this document.)

| Asset  | Asset Name | Description | Trust Attributes |
| --- | --- | --- | --- |
| A-01 | Red authority administration keys | Keys and credentials Red uses to administer the permissioned L1 (for example, validator appointment/removal and parameter changes). | High-impact privileged keys; should be held only by Red; compromise is catastrophic. |
| A-02 | Node 1 validator infrastructure (Alice) | `redbridge01` host running AvalancheGo and Zebra for Node 1 consensus and oracle ingestion. | Node 1 validator keys; host root credentials; service configuration integrity. |
| A-03 | Node 2 validator infrastructure (Bob) | `redbridge02` host running AvalancheGo and Zebra for Node 2 consensus and oracle ingestion. | Node 2 validator keys; host root credentials; service configuration integrity. |
| A-04 | Node 3 validator infrastructure (Carol) | `redbridge03` host running AvalancheGo and Zebra for Node 3 consensus and oracle ingestion. | Node 3 validator keys; host root credentials; service configuration integrity. |
| A-05 | RPC Proxy 1 (Alice) | `redbridge05` host exposing ZavaX RPC Proxy for Node 1 queries. | Proxy host root credentials; TLS private key; ACL and rate-limit policy integrity. |
| A-06 | RPC Proxy 2 (Bob) | `redbridge06` host exposing ZavaX RPC Proxy for Node 2 queries. | Proxy host root credentials; TLS private key; ACL and rate-limit policy integrity. |
| A-07 | RPC Proxy 3 (Carol) | `redbridge07` host exposing ZavaX RPC Proxy for Node 3 queries and public HTTPS access. | Proxy host root credentials; TLS private key; ACL and rate-limit policy integrity. |
| A-08 | Public web server (Walter) | `redbridge04` host serving the public ZavaX Oracle UI to users. | Web server root credentials; TLS private key; deployment/update pipeline integrity. |
| A-09 | Unpublished web server (Xavier) | `redbridge08` host serving a fallback UI endpoint not publicly advertised by default. | Web server root credentials; TLS private key; deployment/update pipeline integrity; endpoint secrecy and controlled disclosure. |
| A-10 | ZavaX Oracle chain state | Canonical on-chain oracle records agreed by validator consensus. | Consensus integrity threshold (>67% voting weight); append-only history assumptions. |
| A-11 | Zebra data feed inputs | Zcash ground-truth data from each node's Zebra instance, consumed for oracle minting decisions. | Source authenticity and consistency across nodes; confirmation-depth policy integrity. |
| A-12 | A monitoring and incident-response capability | Reconciliation, alerting, verification, and incident-coordination capability used to compare RPC outputs, check Zcash ground-truth data, and direct response actions. Available to the entire ZavaX Oracle Community. | Monitoring integrity; alert fidelity; access control for incident tooling; availability during attack and recovery. |

### Security Properties

The following security properties are referenced throughout the usage scenarios:

- **SP‑Availability:** Oracle query and response paths remain operational for expected traffic.
- **SP‑Authentication:** Components can verify peer identity over authenticated channels.
- **SP‑Integrity:** Returned oracle data and on-chain state remain unmodified and correct.
- **SP‑Confidentiality:** Secrets (private keys and credentials) are not exposed to unauthorized parties.

### Adversaries

Adversary **Eve** has the ability to monitor communications on the Internet.

Adversaries **MalloryA**, **MalloryB**, and **MalloryC** can commandeer root control of some amount of nodes from Alice, Bob, and/or Carol due to their poor op-sec.

Adversary **Trudy** can commandeer root control of Walter's web server but is unaware of Xavier's.

Adversaries **DaveA**, **DaveB**, and **DaveC** have botnets big enough to perform a substantial DDoS attack on blockchain nodes, RPC proxy servers, web servers, and so on.

Adversary **Sybil** has many Zcash mining rigs that have been offline and now are about to be repurposed to cause a chain split that introduces fraudulent transactions and has enough electricity available to sustain this attack for up to sixty minutes.

Adversary **Kash** has 10 BTC that she will use to try to bribe Alice, Bob, and Carol to halt or corrupt ZavaX Oracle.

Adversary **Guy** is in the government and tries to coerce Alice, Bob, and Carol to halt or corrupt ZavaX Oracle.


## Overview of ZavaX Oracle Under Normal Operations

The [ZavaX Oracle](https://zavax-oracle.red.dev) is a proof-of-authority (permissioned) Avalanche L1 operating on the Avalanche Fuji Test Network that uses the Snowman++ consensus algorithm. Its purpose is to serve as an oracle of blocks written to the Zcash mainnet chain, reaching consensus and recording these in its own chain. Here is what the website UI looks like:

![Can't display the website screenshot](images/zavaxoraclewebsite.png)

Under normal operations, Oscar can query a Zcash block height using any of the RPC proxy server nodes on the ZavaX Oracle platform using Walter's [website](https://zavax-oracle.red.dev).
- If the contents of the Zcash block at the block height has already been written to the ZavaX chain, it is displayed.
- If it has not yet been written to the ZavaX chain, nodes 1, 2, and 3 reach consensus with each other about the contents of the block by querying their own Zebra daemon and comparing results. Upon reaching consensus, they all write the block contents to their copy of the ZavaX chain, and then they are displayed.
- Nodes reach consensus based on their weight:
  - **Node 1 (Alice's node) has a weight of 50 (41.7% of the total weight)**
  - **Node 2 (Bob's node) has a weight of 40 (33.3% of the total weight)**
  - **Node 3 (Carol's node) has a weight of 30 (25.0% of the total weight)**
- If the block height does not exist yet, is invalid, or is still less than 24 blocks deep, an error message is displayed.

In addition, Oscar may send HTTPS *curl* commands to query a block height as shown in the web UI to Carol's RPC proxy server (the only RPC proxy server configured to allow RPC requests from anywhere) and a response is returned, following the same rules. Walter's website displays valid *curl* commands in the **Request** section.

## The ZavaX Oracle Platform
### Platform Limitations - Please Note
The ZavaX Oracle is a proof-of-concept platform, and as such, it is assumed to have some limitations with respect to this threat model that a more complex blockchain would not have:
- As is the case with many special-purpose blockchains used by the financial industry, it is a proof-of-authority blockchain. The sole authority is the participant Red, and as such, Red has complete control over who has permission to validate the blockchain and their voting weights. Red has chosen only participants Alice, Bob, and Carol to validate the blockchain.
- As a proof-of-authority blockchain, there are no tokens to be staked, and therefore no token economics to consider, including token-based rewards and/or penalties. Instead, the nodes' voting weights are determined solely by Red.
- ZavaX Oracle consists of exactly three nodes. We can assume that no nodes will join the platform, and no nodes will cease operations, except when under attack in various threat scenarios described below.
- The three nodes have *different voting weights*, as described above, so threat consequences vary depending on which nodes are threatened.
- We can assume that when not under attack, participants operate their nodes, RPC proxy servers, and web servers virtuously to keep systems online and, barring external influences, are successful.

With these limitations in mind, we have chosen to describe threats with respect to the three participants operating their particular nodes with their particular voting weights rather than making broader generalizations about "general voting weights n," for instance.

### Platform Diagram
This UML platform diagram shows ZavaX Oracle's three nodes with their associated RPC proxies (operated by Alice, Bob, and Carol) and two web servers (operated by Walter and Xavier), and the connections between them. It omits showing firewalls, but in fact, each host is protected by both host-based and external firewalls, [explained elsewhere](deployment-notes.md).

![Can't display the diagram](images/ZavaXOracleDeployment.png)

As a small Avalanche L1 with only three nodes, it is well-suited to illustrate some worst-case threat model scenarios. One can see that all of these attacks' feasibility and impact could be mitigated—often by orders of magnitude—*by increasing the size of the network and distribution of stake.*

Finally, it's worth noting that in all but two of the scenarios described here, no bad data can be written to the ZavaX Oracle blockchain. Indeed, only under **the most severe attacks**, where many nodes together with greater than 67% of stake are commandeered due to lax op-sec and/or coercion, or the entire network is compromised, is it possible to actually write bad oracle data to the blockchain. Even in these two extreme cases, the blockchain can be reclaimed by virtuous operators within 24 hours.

Now let's explore threat model scenarios, security invariants, known weaknesses, and mitigations. Usage scenarios we examine include monitoring by an adversary, increasingly harsh infrastructure hacking, denial-of-service attacks, oracle attacks (the Zcash network is the oracle), and finally the theft of private keys that control the permissioned (proof-of-authority) ZavaX Oracle network. Regarding bribery by Kash or coercion by Guy, we focus on the impact of bribing or coercing the node operators Alice, Bob, Carol, as well as Red.

## Usage-Scenario-to-Assets-and-Properties Matrix

| Usage Scenario | Description | Violated Properties | Impacted Assets | Adaptive Behavior Relevant? |
| --- | --- | --- | --- | --- |
| US-01 | Normal Operations with Monitoring | none | none | No |
| US-02 | MalloryA gains root access to Carol's Infrastructure | SP‑Authentication, SP‑Integrity (off-chain responses), SP‑Confidentiality | A-04, A-07 | Yes |
| US-03 | MalloryB gains root access to both Bob's and Carol's ZavaX Infrastructure | SP‑Authentication, SP‑Integrity (off-chain responses), SP‑Confidentiality | A-03, A-04, A-06, A-07 | Yes |
| US-04 | MalloryC gains root access to both Alice's and Bob's ZavaX infrastructure | SP‑Authentication, SP‑Integrity (on-chain and off-chain), SP‑Confidentiality | A-02, A-03, A-05, A-06, A-10 | Yes |
| US-05 | Trudy gains root access to Walter's web server that runs the ZavaX UI | SP‑Authentication, SP‑Integrity (UI responses), SP‑Confidentiality | A-08 | Yes |
| US-06 | DaveA attempts to perform a DDoS attack against one, two, or three RPC proxy servers | SP‑Availability | A-05, A-06, A-07 (and dependent access via A-08, A-09) | No |
| US-07 | DaveB attempts to perform a DDoS attack against one, two, or three ZavaX nodes | SP‑Availability | A-02, A-03, A-04 (and dependent outage on A-05, A-06, A-07) | No |
| US-08 | DaveC attempts to perform a DDoS attack against Walter's web server | SP‑Availability | A-08 (and possible follow-on risk to A-09) | No |
| US-09 | Sybil attempts to cause a Zcash chain split that includes all three ZavaX nodes for one hour | SP‑Integrity (risk after depth window) | A-11 with downstream risk to A-10 | Yes |
| US-10 | Red's private keys for administering the permissioned ZavaX Oracle L1 are stolen | SP‑Availability, SP‑Authentication, SP‑Integrity, SP‑Confidentiality | A-01, A-10, A-02, A-03, A-04 | Yes |
| n/a | Bribery/coercion variations (Kash/Guy) | Same as matched base scenario | Same as matched base scenario | Yes |

## Usage Scenario US-01: Normal Operations with Monitoring

Eve is monitoring all network traffic between all devices in the ZavaX Oracle platform, all Zcash nodes, and Avalanche nodes. In other words, the Internet is functioning normally.

### Referenced Security Properties and Assets

Security properties referenced in this scenario: **SP‑Availability**, **SP‑Authentication**, **SP‑Integrity**, **SP‑Confidentiality**.

Assets in scope:
- **A-02**, **A-03**, **A-04:** Validator infrastructures
- **A-05**, **A-06**, **A-07:** RPC proxy infrastructures
- **A-08**, **A-09:** Web server infrastructures
- **A-10:** ZavaX Oracle chain state
- **A-11:** Zebra data feed inputs
- **A-12:** Monitoring and incident-response capability

### Security Invariants

Even with complete monitoring, the ZavaX Oracle platform will operate normally.

All connections except for Zebra-to-Zebra connections are performed over TLS with a certificate issued by [Let's Encrypt](https://letsencrypt.org) and are therefore secure from Eve's snooping to the extent that this configuration provides. The TLS private key is stored in a non-public location on the servers; the public key is available to all.

Properties violated in this scenario: **none**.

Impacted assets in this scenario: **none** (no loss of control or integrity for A-02 through A-12).

### Known Weaknesses

If Oscar were to perform many queries in a short time, for instance by running automation software, some queries would fail due to rate limits imposed by the RPC proxy nodes.

Also, Oscar cannot use *curl* to query nodes 1 and 2 due to the security preferences that Alice and Bob have chosen to use for their RPC nodes. However, Carol's node 3 is configured for rate-limited *curl* use by anyone.

If Oscar exceeds rate limits, this is a constrained **SP‑Availability** limitation on **A-05**, **A-06**, and **A-07**.

### Mitigations

No additional security mitigation is required beyond documented rate limits.

## Usage Scenario US-02: MalloryA gains root access to Carol's Infrastructure

### Referenced Security Properties and Assets

Security properties referenced in this scenario: **SP‑Availability**, **SP‑Authentication**, **SP‑Integrity**, **SP‑Confidentiality**.

Assets in scope:
- **A-04:** Node 3 validator infrastructure (Carol)
- **A-07:** RPC Proxy 3 (Carol)
- **A-02**, **A-03:** Node 1 and Node 2 validator infrastructures (Alice and Bob)
- **A-05**, **A-06:** RPC Proxy 1 and RPC Proxy 2 (Alice and Bob)
- **A-10:** ZavaX Oracle chain state
- **A-11:** Zebra data feed inputs
- **A-12:** Monitoring and incident-response capability

### Security Invariants

Alice and Bob's nodes 1 and 2 continue to function normally, acting as oracles for already-minted data and correctly minting new oracle data to the ZavaX Oracle chain. This is because together, Alice and Bob control 75% of stake, greater than the 67% needed.

Properties preserved in this scenario are partial **SP‑Availability** (platform remains available through Alice/Bob paths) and **SP‑Integrity** for **A-10** (canonical chain state remains correct because attacker controls less than 67% voting weight).

### Known Weaknesses

This is a **serious attack**. In this usage scenario, MalloryA will be able to provide incorrect results to Oscar via Walter's web server when the web server submits a query to Carol's RPC proxy 3 for any block.

Properties violated in this scenario:
- **SP‑Availability** (partial: platform is no longer available through Carol's path).
- **SP‑Authentication** on **A-04** and **A-07** (attacker has root access and can impersonate trusted services from Carol's infrastructure).
- **SP‑Integrity** of responses served by **A-07** (false results can be returned to clients through Carol's RPC path).
- **SP‑Confidentiality** on **A-04** and **A-07** (local credentials, keys, and operational secrets can be exposed after root compromise).

Impacted assets in this scenario: **A-04** and **A-07** directly; client trust in responses from Carol's endpoint is degraded. **A-10** remains uncorrupted in this scenario.

**Note:** Of course, MalloryA could pretend to be a virtuous actor for a long time before attacking, biding her time while providing correct results; this **Known Weakness** describes what happens when MalloryA decides to stop acting virtuously and attacks. (The same is also true for the attackers in the rest of the **Usage Scenarios**.)

### Mitigations

**Detector (who notices):** Oscar, Red, and Carol, using **A-12**, by running continuous reconciliation across Alice's, Bob's, and Carol's RPC outputs and detecting when one path diverges from the other two. Oscar may also notice anomalous results independently and report them to Red.

**Signal (what they notice):** One RPC path returns data that does not match the other two, indicating that some part of the platform has been compromised and strongly suggesting compromise of Node 3 / Carol's RPC path when Nodes 1 and 2 agree. Observers may also notice that Carol's node is proposing blocks whose contents do not match the canonical Zcash chain's block data.

**Verifier (who confirms):** Red and Carol, using **A-12**, by checking Zcash ground-truth data directly to confirm that the matching results from Alice and Bob are correct and that Carol's divergent result is the compromised one.

**Decision-maker (who acts):** Red and Carol, by directing Carol's recovery actions and coordinating any validator or trust-restoration steps required on this permissioned proof-of-authority chain after they confirm the compromise. Oscar may also report the problem to Red if he encounters it first.

**Recovery trigger (when to return):** Return Carol's infrastructure and RPC path to normal trust only after Carol has re-secured control of the affected systems and their results again match Alice's and Bob's responses as well as Zcash ground-truth data.

**Prevention (how to reduce recurrence):** This scenario can be prevented in the first place by implementing normal server op-sec: Carol should keep her SSH private keys secure and password-protected, configure firewalls to allow logins only from a narrow range of IP addresses, and apply comparable hardening controls to reduce the chance of host compromise.

### Variation: Kash Bribes or Guy Coerces Carol

The **Security Invariants** and **Known Weaknesses** remain the same. However, the **Mitigations** are different.

**Detector:** Red and Oscar, by observing Carol's repeated bad behavior, inconsistent operator conduct, or sustained response anomalies that indicate Carol is no longer acting as a trustworthy node operator.

**Signal:** Carol's behavior (specifically, Carol's node is proposing bad blocks) repeatedly departs from expected honest operation in a way that cannot be explained by transient faults, creating a credible inference that she has been bribed or coerced rather than merely compromised at the host level.

**Verifier:** Red, by comparing Carol's behavior against Alice's and Bob's expected results, checking Zcash ground-truth data as needed, and confirming that the pattern warrants operator replacement rather than simple infrastructure recovery.

**Decision-maker:** Red decides that Carol has been compromised as an operator and replaces her with a new node operator.

**Recovery trigger:** Return the Carol-operated role to normal trust only after the replacement node operator has been installed, the new infrastructure is under trusted control, and the replacement path produces results consistent with the rest of the platform and Zcash ground-truth data.

**Prevention:** This scenario can be prevented in the first place by providing Carol with protections and incentives strong enough so that she remains a reliable node operator that cannot be altered by Kash or Guy.

Properties violated in this variation: same as above (**SP‑Authentication**, **SP‑Integrity** response integrity, and potentially **SP‑Confidentiality** on Carol-controlled assets), with impacted assets **A-04** and **A-07**.

## Usage Scenario US-03: MalloryB gains root access to both Bob's and Carol's ZavaX Infrastructure

### Referenced Security Properties and Assets

Security properties referenced in this scenario: **SP‑Availability**, **SP‑Authentication**, **SP‑Integrity**, **SP‑Confidentiality**.

Assets in scope:
- **A-03**, **A-04:** Node 2 and Node 3 validator infrastructures (Bob and Carol)
- **A-06**, **A-07:** RPC Proxy 2 and RPC Proxy 3 (Bob and Carol)
- **A-02**, **A-05:** Node 1 and RPC Proxy 1 (Alice) as independent cross-check path
- **A-10:** ZavaX Oracle chain state
- **A-11:** Zebra data feed inputs
- **A-12:** Monitoring and incident-response capability

### Security Invariants

Querying Alice's node for data in already-minted blocks will produce correct results. Inconsistent results from Alice's node in comparison to those from Bob's and Carol's would indicate to Oscar and others that the ZavaX platform is under attack and compromised.

Properties preserved in this scenario are partial **SP‑Availability** through Alice's path (**A-02** and **A-05**) and **SP‑Integrity** for **A-10** (canonical chain state remains correct because attacker still controls less than 67% voting weight).

### Known Weaknesses

This is a **severe attack**, with 58.3% of stake controlled by an attacker. Because the 67% threshold to mint new blocks has not been reached, no new blocks with incorrect data can be minted. However, Bob's and Carol's RPC servers can make it *appear* to Walter's web server that new blocks have been minted and can provide incorrect data. Still, Alice's RPC server would produce conflicting results, indicating to Oscar that there's a problem.

Properties violated in this scenario:
- **SP‑Availability** (partial: platform is no longer available through Bob's and Carol's paths).
- **SP‑Authentication** on **A-03**, **A-04**, **A-06**, and **A-07** (attacker can impersonate trusted operators/services on compromised hosts).
- **SP‑Integrity** for responses served via **A-06** and **A-07** (false oracle responses can be presented off-chain).
- **SP‑Confidentiality** on **A-03**, **A-04**, **A-06**, and **A-07** (credentials and local secrets may be exposed post-compromise).

Impacted assets in this scenario: **A-03**, **A-04**, **A-06**, **A-07** directly; **A-10** remains uncorrupted.

### Mitigations

**Detector:** Oscar, Red, Alice, Bob, and Carol, using **A-12**, by running continuous reconciliation across Alice's, Bob's, and Carol's RPC outputs and detecting when Alice's path diverges from Bob's and Carol's matching responses. Oscar may also notice anomalous results independently and report them to Red.

**Signal:** Alice's RPC path returns results that conflict with Bob's and Carol's matching responses, indicating that some part of the platform has been compromised and strongly suggesting compromise of Bob's and Carol's infrastructure when their two paths agree against Alice. Observers may also notice that Bob and Carol's nodes are proposing blocks whose contents do not match the canonical Zcash chain.

**Verifier:** Oscar, Red, Bob, and Carol, using **A-12**, by checking Zcash ground-truth data directly to confirm that Alice's results match ground-truth data and that Bob's and Carol's divergent responses are the compromised ones.

**Decision-maker:** Red, Bob, and Carol, by directing Bob's and Carol's recovery actions and coordinating any validator or trust-restoration steps required on this permissioned proof-of-authority chain after they confirm the compromise.

**Recovery trigger:** Return Bob's and Carol's infrastructure and RPC paths to normal trust only after they have re-secured control of the affected systems and their results again match Alice's responses as well as Zcash ground-truth data. As in the scenario above, Bob and Carol should be able to complete this recovery within 24 hours.

**Prevention:** This scenario can be prevented in the first place by implementing normal server op-sec: Bob and Carol should keep their SSH private keys secure and password-protected, configure firewalls to allow logins only from narrow ranges of IP addresses, and apply comparable hardening controls to reduce the chance of host compromise.

### Variation: Kash Bribes or Guy Coerces Bob and Carol

The **Security Invariants** and **Known Weaknesses** remain the same. However, the **Mitigations** are different.

**Detector:** Red and Oscar, by observing Bob's and Carol's repeated bad behavior, coordinated response anomalies, or sustained departures from expected honest operator conduct.

**Signal:** Bob's and Carol's behavior (specifically, Bob and Carol's nodes are proposing bad blocks) repeatedly departs from expected operation in a coordinated way that cannot be explained by transient faults, creating a credible inference that both operators have been bribed or coerced.

**Verifier:** Red, by comparing Bob's and Carol's outputs against Alice's independent path, checking Zcash ground-truth data as needed, and confirming that the pattern warrants operator replacement rather than host-level recovery.

**Decision-maker:** Red decides that Bob and Carol have been compromised as operators and replaces them with new node operators.

**Recovery trigger:** Return the Bob-operated and Carol-operated roles to normal trust only after the replacement operators have been installed, the new infrastructure is under trusted control, and the replacement paths again produce results consistent with Alice's path and Zcash ground-truth data.

**Prevention:** This scenario can be prevented in the first place by providing Bob and Carol with protections and incentives strong enough so that they remain reliable node operators that cannot be altered by Kash or Guy.

It is also worth noting that Kash would have to divide her 10 BTC between two people, limiting her bribery power. Similarly, if Bob and Carol are in two different jurisdictions, Guy would have to be able to coerce in both jurisdictions.

Properties violated in this variation: same as above (**SP‑Authentication**, off-chain **SP‑Integrity** response integrity, and potentially **SP‑Confidentiality**), with impacted assets primarily **A-03**, **A-04**, **A-06**, and **A-07**.

## Usage Scenario US-04: MalloryC gains root access to both Alice's and Bob's ZavaX infrastructure

### Referenced Security Properties and Assets

Security properties referenced in this scenario: **SP‑Availability**, **SP‑Authentication**, **SP‑Integrity**, **SP‑Confidentiality**.

Assets in scope:
- **A-02**, **A-03:** Node 1 and Node 2 validator infrastructures (Alice and Bob)
- **A-05**, **A-06:** RPC Proxy 1 and RPC Proxy 2 (Alice and Bob)
- **A-04**, **A-07:** Node 3 and RPC Proxy 3 (Carol) as independent comparison path
- **A-10:** ZavaX Oracle chain state
- **A-11:** Zebra data feed inputs
- **A-12:** Monitoring and incident-response capability

### Security Invariants

Querying Carol's node for data in already-minted blocks will produce correct results. Inconsistent results from Carol's node in comparison to those from Alice's and Bob's would indicate to Oscar and others that the ZavaX Oracle platform is under attack and compromised.

Property preserved in this scenario: limited **SP‑Integrity** at the comparison endpoint (**A-04** and **A-07**) for already-minted historical data checks.

### Known Weaknesses

This is **the most severe attack**, with 75% of stake controlled by an attacker. Because the 67% threshold to mint new blocks has been reached, new blocks with incorrect data can be minted, leaving Carol's node no choice but to follow consensus rules and endorse a bad state, or to halt, or to fall out of consensus.

Properties violated in this scenario:
- **SP‑Availability** (partial: platform is no longer available through Alice's and Bob's path, and Carol's path only replies with correct oracle information for already-minted blocks).
- **SP‑Authentication** on **A-02**, **A-03**, **A-05**, and **A-06** (attacker controls trusted infrastructure).
- **SP‑Integrity** on **A-10** (bad oracle data can be written on-chain with majority control).
- **SP‑Integrity** of responses via **A-05** and **A-06** (incorrect responses can be served to clients).
- **SP‑Confidentiality** on **A-02**, **A-03**, **A-05**, and **A-06** (secrets and credentials may be exposed).

Impacted assets in this scenario: **A-02**, **A-03**, **A-05**, **A-06**, and critically **A-10**.

### Mitigations

**Detector:** Oscar, Red, Alice, Bob, and Carol, using out-of-band coordination, with Red using **A-12**, by comparing responses from Carol's path (**A-04** and **A-07**) against Alice's and Bob's paths and watching for divergence from expected results.

**Signal:** Carol's independent path disagrees with Alice's and Bob's paths, and the divergence is confirmed against Zcash ground-truth data (**A-11**) and recent on-chain state (**A-10**), indicating majority compromise of Alice's and Bob's infrastructure and possible corruption of chain state. Specifically, observers notice that valid blocks that Carol's node proposes are rejected.

**Verifier:** Red, Alice, and Bob share verification authority for this response, using out-of-band coordination, with Red and/or others acting through **A-12**, by confirming the scope of compromise, identifying the compromised block window, and validating the divergence against both Zcash ground-truth data (**A-11**) and trusted checkpoints or snapshots of **A-10**.

**Decision-maker:** Red, Alice, and Bob share incident-command and validator-replacement decision authority for this response. Using out-of-band coordination, they remove compromised validators from active participation, disable or firewall compromised RPC endpoints (**A-05**, **A-06**) to stop serving false data, freeze public UI paths that depend on compromised endpoints until clean data paths are restored, assume all credentials on compromised hosts are exposed, revoke and replace validator, proxy, and admin keys tied to compromised systems, rotate all host credentials, API secrets, and TLS private keys on affected infrastructure, rebuild compromised validator and proxy hosts from known-good images or hardware rather than trusting in-place recovery, restore from the last trusted checkpoint or snapshot before compromise, and re-run oracle minting for the affected range from validated Zcash inputs (**A-11**) to republish corrected state.

**Recovery trigger:** Return to service only after independent reconciliation by Carol's path plus out-of-band checks confirms that rebuilt infrastructure and corrected chain state are consistent, public endpoints can be safely re-enabled, and degraded mode has remained in place until consistency checks pass for a defined stabilization window.

**Prevention:** After recovery, harden **A-12** with continuous cross-node response reconciliation, stricter admin-path controls, and periodic incident-response drills for this exact majority-compromise case.

### Variation: Kash Bribes or Guy Coerces Alice and Bob

The **Security Invariants** and **Known Weaknesses** remain the same. However, the **Mitigations** are different.

**Detector:** Red and Oscar, by observing Alice's and Bob's repeated bad behavior, coordinated deviations from expected validator conduct, or sustained anomalies in the data they serve.

**Signal:** Alice's and Bob's behavior repeatedly departs from expected honest operation in a coordinated way that cannot be explained by transient faults, creating a credible inference that both operators have been bribed or coerced. Specifically, valid blocks that Carol's node proposes are rejected by Alice and Bob's nodes.

**Verifier:** Red, by comparing Alice's and Bob's behavior against the remaining independent path, checking Zcash ground-truth data and chain effects as needed, and confirming that the pattern warrants operator replacement rather than routine infrastructure recovery.

**Decision-maker:** Red decides that Alice and Bob have been compromised as operators and replaces them with new node operators.

**Recovery trigger:** Return the Alice-operated and Bob-operated roles to normal trust only after the replacement operators have been installed, the new infrastructure is under trusted control, and the replacement paths again produce results consistent with the independent comparison path and trusted ground truth.

It is also worth noting, as above, that Kash would have to divide her 10 BTC between two people, limiting her bribery power. Similarly, if Alice and Bob are in two different jurisdictions, Guy would have to be able to coerce in both jurisdictions.

Properties violated in this variation: same as above, including **SP‑Integrity** violation for **A-10**.

## Usage Scenario US-05: Trudy gains root access to Walter's web server that runs the ZavaX UI

### Referenced Security Properties and Assets

Security properties referenced in this scenario: **SP‑Availability**, **SP‑Authentication**, **SP‑Integrity**, **SP‑Confidentiality**.

Assets in scope:
- **A-08:** Public web server (Walter)
- **A-09:** Unpublished web server (Xavier)
- **A-07:** RPC Proxy 3 (public `curl` cross-check path)
- **A-10:** ZavaX Oracle chain state

### Security Invariants

While Walter's web server could produce incorrect results, Xavier's unpublished web server would remain functioning well, as would **curl** commands to Carol's RPC proxy server. (Note: There is no information about the existence of Xavier's web server for Trudy to find on Walter's web server.)

Properties preserved in this scenario: **SP‑Availability** (service remains available through **A-09** and **A-07**) and **SP‑Integrity** for **A-10** (chain state remains unchanged).

### Known Weaknesses

There would be no indication of Walter's web server malfunctioning without running a cross-check.

Properties violated in this scenario:
- **SP‑Authentication** on **A-08** (attacker can impersonate the trusted UI service).
- **SP‑Integrity** of UI-delivered responses from **A-08** (misleading results can be shown to users).
- **SP‑Confidentiality** on **A-08** (server-side credentials and secrets may be exposed).

Impacted assets in this scenario: **A-08** directly; **A-10** remains unmodified.

### Mitigations

**Detector:** Operators, Oscar, Red, or Xavier, by noticing inconsistent results on Walter's web server (**A-08**) when compared with other available query paths.

**Signal:** Divergence between data shown on Walter's web server and the same data returned by Xavier's unpublished web server or by RPC calls to Carol's RPC proxy server (**A-07**).

**Verifier:** Xavier, Red, and independent users, by cross-checking Walter's published results against Xavier's web server and Carol's RPC responses before treating Walter's UI output as trustworthy.

**Decision-maker:** Xavier, in consultation with Red, decides to publicize his unpublished web server to the ZavaX Oracle community as an alternate trusted web access path while Walter's server remains suspect.

**Recovery trigger:** Resume relying on Walter's web server as a primary public interface only after its results consistently match Xavier's web server and Carol's RPC proxy server over a stabilization period.

## DDoS Assumptions

These assumptions apply to all DDoS scenarios below:

- Attack traffic is assumed **not** to congest upstream ISP links for node, proxy, or web-server operators.
- Host-level and provider-level filtering remain available during attack.
- Operators retain out-of-band coordination channels (for example: phone/Signal and private operations chat).
- **[Vultr.com](https://www.vultr.com/) is an example ISP/infrastructure provider that offers additional DDoS services**, including optional per-instance [DDoS Protection](https://docs.vultr.com/ddos-protection/), automatic mitigation, and network-edge filtering.

## Usage Scenario US-06: DaveA attempts to perform a DDoS attack against one, two, or three RPC proxy servers

### Referenced Security Properties and Assets

Security properties referenced in this scenario: **SP‑Availability**, **SP‑Integrity**.

Assets in scope:
- **A-05**, **A-06**, **A-07:** RPC proxy infrastructures
- **A-08**, **A-09:** Web servers dependent on RPC paths
- **A-10:** ZavaX Oracle chain state

### Security Invariants

The nodes would keep working, reaching consensus and minting new blocks. Other RPC proxy servers, if allowed by the nodes' firewalls, would remain online.

Because almost all of the load for the attack would be handled by the server's external firewall and openresty filtering, the proxy service itself would remain operating and could be configured to provide services through hidden channels that were not under attack.

Property preserved in this scenario: **SP‑Integrity** for **A-10** (consensus and block minting continue even if some proxy endpoints are degraded).

### Known Weaknesses

If all RPC nodes are successfully attacked at once, Walter's web server would be unable to function as an oracle, and *curl* commands sent to Carol's RPC proxy server would not receive responses.

Property violated in this scenario: **SP‑Availability** for **A-05**, **A-06** and **A-07** and dependent user access paths via **A-08** and **A-09**.

Impacted assets in this scenario: primarily **A-05**, **A-06**, **A-07**; second-order impact on oracle query usability through **A-08** and **A-09**.

### Mitigations

**Detector:** RPC proxy operators, by monitoring request rate, latency, error rate, and dropped traffic per proxy.

**Signal:** Success rate falls below the defined threshold for more than 10 minutes, indicating that one or more public RPC proxy endpoints are under active DDoS pressure.

**Verifier:** Proxy operators and dependent service owners, by confirming that the degradation is confined to public endpoint availability while emergency allowlisted RPC paths remain reachable and **A-10** consensus integrity remains intact.

**Decision-maker:** The affected proxy operators, coordinating with Red, Walter, and Xavier, declare the incident, move trusted traffic to freshly-created allowlisted emergency RPC endpoints, enable additional provider DDoS controls, tighten firewall/WAF/rate limits, and place the web UI into degraded mode with clear status messaging while serving cached results for already-finalized heights where possible.

**Recovery trigger:** Remove degraded mode notifications in the web UI only after a defined stability window shows normal request success, latency, and error rates. After incident close, rotate temporary emergency endpoint credentials and URLs.

## Usage Scenario US-07: DaveB attempts to perform a DDoS attack against one, two, or three ZavaX nodes

### Referenced Security Properties and Assets

Security properties referenced in this scenario: **SP‑Availability**, **SP‑Integrity**.

Assets in scope:
- **A-02**, **A-03**, **A-04:** validator node infrastructures
- **A-05**, **A-06**, **A-07:** RPC proxies dependent on node connectivity
- **A-10:** ZavaX Oracle chain state

### Security Invariants

The nodes would keep functioning internally but could be blocked from reaching consensus and minting new blocks. RPC proxy servers would not be able to communicate with their nodes.

Because almost all of the load for the attack would be handled by the node's external firewall, the node itself would remain operating and could be configured to connect with other nodes through hidden channels that were not under attack.

Property preserved in this scenario: **SP‑Integrity** of previously finalized records in **A-10**.


### Known Weaknesses

If all ZavaX nodes are attacked at once, contact between each node and its RPC proxy server could be disrupted, and as a result, Walter's web server would be unable to function as an oracle, and *curl* commands sent to Carol's RPC proxy server would not receive responses.

Property violated in this scenario: **SP‑Availability** for **A-02**, **A-03** and **A-04** and dependent RPC/query paths (**A-05**, **A-06** and **A-07**).

Impacted assets in this scenario: **A-02**, **A-03**, **A-04** directly; secondary service outage effects on **A-05**, **A-06**, and **A-07**.

### Mitigations

**Detector:** Node operators, Red, and Oscar, by alerting on peer collapse, consensus stalls, and node-to-proxy disconnects.

**Signal:** Quorum risk is detected because one or more validators become unreachable or node-to-node and node-to-proxy communication paths fail under active attack.

**Verifier:** Node operators and Red, by confirming that the disruption is an availability failure rather than an integrity failure, and by validating whether emergency allowlisted node communication paths remain usable for restored coordination.

**Decision-maker:** Red, coordinating with node operators, triggers the emergency response, switches node-to-node and node-to-proxy traffic to preconfigured alternate allowlisted communication paths such as VPN or tunnel links, may temporarily remove unreachable validators and appoint standby validator capacity, enables additional provider DDoS services for validator hosts, and tightens pre-staged node firewall policies.

**Recovery trigger:** Return the normal validator topology and communication paths only after rebuild where needed, verification that quorum and connectivity are stable, and a completed stability check window. Standby infrastructure must remain patched, minimal, and ready for fast activation until recovery is complete.

## Usage Scenario US-08: DaveC attempts to perform a DDoS attack against Walter's web server

### Referenced Security Properties and Assets

Security properties referenced in this scenario: **SP‑Availability**, **SP‑Integrity**.

Assets in scope:
- **A-08:** Public web server (Walter)
- **A-09:** Unpublished web server (Xavier)
- **A-05**, **A-06**, **A-07:** RPC proxy alternatives for direct checks
- **A-10:** ZavaX Oracle chain state

### Security Invariants

Because almost all of the load for the attack would be handled by the server's external firewall and openresty filtering, depending on the severity of the attack, the web server may continue to function normally.

All RPC proxy servers and nodes remain operating normally. Xavier's unpublished web server also remains operating normally.

Properties preserved in this scenario: **SP‑Integrity** for **A-10** and partial **SP‑Availability** via **A-09** and direct RPC access.

### Known Weaknesses

Once Xavier's web server's address is publicized to the ZavaX Oracle community, it could be a target for attack as well.

Property violated in this scenario (when attack succeeds): **SP‑Availability** for **A-08**.

Impacted assets in this scenario: **A-08** directly; possible follow-on availability risk to **A-09** if it is revealed.

### Mitigations

**Detector:** Oscar, Red, Walter, Xavier, and other operators, by monitoring Walter's public web endpoint for abnormal request volume, service degradation, failed health checks, and availability loss.

**Signal:** Walter's public web server becomes unavailable or unstable under volumetric floods or host-targeted traffic spikes, while the rest of the ZavaX Oracle platform remains healthy.

**Verifier:** Red, Walter, Xavier, and other operators, by confirming that the outage is isolated to Walter's public web server and not caused by failures in the RPC proxy servers, validator nodes, or upstream chain state.

**Decision-maker:** Under Red's direction, Walter applies the prepared DDoS response for the public web server, including enabling additional provider protections and tightening firewall, WAF, rate-limit, and bot-filtering controls as needed. If Walter's public endpoint cannot be restored promptly, Xavier publicizes his unpublished web server as a fallback access path for the ZavaX Oracle community. Operators limit disclosure of fallback endpoint details to what is operationally necessary and maintain separate credentials and certificates for fallback infrastructure.

**Recovery trigger:** Resume normal reliance on Walter's public web server only after its availability stabilizes, attack symptoms subside, and the service passes a defined stability check window. If Xavier's fallback endpoint was publicized during the incident, operators should treat it as newly exposed and reassess whether to keep using it, de-publicize it, or prepare a fresh unpublished fallback endpoint.

## Usage Scenario US-09: Sybil attempts to cause a Zcash chain split that includes all three ZavaX nodes for one hour

### Referenced Security Properties and Assets

Security properties referenced in this scenario: **SP‑Availability**, **SP‑Integrity**.

Assets in scope:
- **A-11:** Zebra data feed inputs
- **A-10:** ZavaX Oracle chain state
- **A-02**, **A-03**, **A-04:** validator nodes that consume the feed

### Security Invariants

Because the ZavaX Oracle will not include a Zcash block until it is over 24 blocks deep, during those approximately 30 minutes (depending on Zcash block interval during this attack), the ZavaX Oracle would operate normally.

Property preserved in this phase: **SP‑Integrity** for **A-10**, because confirmation-depth policy protects against short-lived chain splits.

### Known Weaknesses

After approximately 30 minutes, the ZavaX Oracle blockchain would be in danger of reporting bad data.

Property at risk in this scenario: **SP‑Integrity** for **A-10**, due to compromised trustworthiness of **A-11** under a sustained chain split.

Impacted assets in this scenario: **A-11** as corrupted external input source, with downstream integrity risk to **A-10**.

### Mitigations

**Detector:** Alice, Bob, and Carol, through continuous monitoring of the Zcash blockchain and their independent Zebra views.

**Signal:** A sustained Zcash chain split approaching or exceeding the ZavaX safety window, with conflicting branch histories that could corrupt future oracle writes after the 24-block confirmation threshold.

**Verifier:** Alice, Bob, and Carol, by comparing their observed Zcash branch state across independent nodes before halting ZavaX block production.

**Decision-maker:** Decisions depend on when the threat is discovered and whether or not it is ongoing.
- If it is discovered within the 30-minute window, Alice, Bob, and Carol jointly decide to temporarily halt ZavaX Oracle block production until the Zcash chain split resolves. 
- If it is discovered after the 30 minute window and before the attack has ended, Alice, Bob, and Carol can halt block production and stop reporting recent blocks. 
- If it is discovered after the attack has ended, or if it is discovered after the 30 minute window, Alice, Bob, and Carol in coordination with Red can jointly decide to roll back their nodes to a block height just prior to when bad blocks were written to the ZavaX Oracle blockchain.

**Recovery trigger:** Resume ZavaX block production only after the Zcash chain has reconverged on a stable canonical branch and the confirmation-depth policy again protects **A-10** from bad upstream data.

While block production is halted, the RPC proxy servers and Walter's and Xavier's web servers can continue serving Zcash blocks already written to the ZavaX chain. New blocks cannot be reported until ZavaX block production resumes.

## Usage Scenario US-10: Red's private keys for administering the permissioned ZavaX Oracle L1 are stolen

### Referenced Security Properties and Assets

Security properties referenced in this scenario: **SP‑Availability**, **SP‑Authentication**, **SP‑Integrity**, **SP‑Confidentiality**.

Assets in scope:
- **A-01:** Red authority administration keys
- **A-10:** ZavaX Oracle chain state
- **A-02**, **A-03**, **A-04:** Validator infrastructures (subject to adversarial reconfiguration)
- **A-12:** Monitoring and incident-response capability

### Invariants and Known Weakness

This is a catastrophic failure of the platform. (However important qualifications apply.)

Properties violated in this scenario:
- **SP‑Availability** may be violated if malicious governance actions halt or destabilize operations.
- **SP‑Integrity** for **A-10** (validator set/parameters can be maliciously altered, undermining chain trust).
- **SP‑Authentication** of governance actions (attacker can issue privileged actions as Red).
- **SP‑Confidentiality** on **A-01** (administration keys are exposed).

Impacted assets in this scenario: **A-01** and **A-12** directly and system-wide impact on **A-10** and validator operations (**A-02**, **A-03** and **A-04**).

### Mitigations

**Detector:** Oscar, Red, validator operators, and dependent service operators, by detecting unauthorized governance or administration actions attributable to Red's stolen keys.

**Signal:** Privileged actions on the permissioned ZavaX Oracle L1 occur without authorization, indicating that Red's administration keys (**A-01**) have been compromised, that **A-12** can no longer be treated as authoritative without revalidation, and that chain governance can no longer be trusted.

**Verifier:** Red and the remaining operators, by confirming the key compromise using out-of-band coordination, identifying the malicious governance actions taken, and validating that Alice, Bob, Carol, Walter, and Xavier still retain control of their own infrastructure.

**Decision-maker:** Red, coordinating with Alice, Bob, Carol, Walter, and Xavier, using out-of-band coordination, initiates an orderly rebuild of the platform and blockchain, including bringing down and/or rebuilding the affected web servers, proxy servers, and nodes as needed. No data is expected to be lost, but new blocks cannot be reported until ZavaX block production resumes.

**Recovery trigger:** Return the rebuilt platform to service only after the replacement environment is fully synchronized, trusted administration control has been re-established, and validator and dependent services have been rebuilt and verified. Recovery is expected to take up to about one week, with the limiting step being initial synchronization of a new Zebra instance with the Zcash blockchain.

Only proof-of-authority blockchains have this vulnerability; in proof-of-stake blockchains, there is no central authority like Red.

### Variation: Kash Bribes or Guy Coerces Red

The **Security Invariants** and **Known Weaknesses** remain the same. However, the **Mitigations** are different.

**Detector:** Since Red has been corrupted, only Oscar and the ZavaX Oracle Community as a group could detect this attack. They would do so by detecting governance or administration actions attributable to Red, or by Community members independently verifying misleading ZavaX Oracle results with the canonical Zcash blockchain.

**Signal:** Privileged actions on the permissioned ZavaX Oracle L1 occur seemingly in opposition to the best interests of the ZavaX Oracle community, for instance, the removal of Alice's node without cause (just one of many examples), indicating that Red may have been compromised by bribery or coercion, that **A-12** can no longer be treated as authoritative without revalidation, and that chain governance can no longer be trusted.

**Verifier:** The ZavaX Oracle Community, through ad hoc means.

**Decision-maker:** The ZavaX Oracle Community initiates an orderly fork of the platform and blockchain without Red, including bringing down and/or rebuilding the affected web servers, proxy servers, and nodes as needed. How exactly the Community decides and does this is left up to them. Canonical source data from the Zcash chain is recoverable, but new blocks cannot be reported until ZavaX block production resumes.

**Recovery trigger:** Return the rebuilt platform to service only after the forked environment is fully synchronized, trusted administration control has been re-established, and validator and dependent services have been rebuilt and verified. Recovery is expected to take up to about one week, with the limiting step being initial synchronization of a new Zebra instance with the Zcash blockchain.

Only proof-of-authority blockchains have this vulnerability; in proof-of-stake blockchains, there is no central authority like Red.

Properties violated in this variation: same as above, including **SP‑Integrity** violation for **A-10**.

## Adversary Collusion

Given these threat scenarios, what if adversaries collude or happen to attack at the same time? For instance, what if MalloryA gains root access to Carol's Infrastructure (US-02) at the same time as Trudy gains root access to Walter's web server that runs the ZavaX UI (US-05)? This is just one example of many possible combinations. Is the effect of such collusion exponential, multiplicative, or just additive? *In all cases given the scenarios described above, colluding threats are just additive.*

Since collusion effects are just additive, in order to map collusion combinations to violated properties, one just needs to combine the violated properties of the two usage scenarios found in both of the *Known Weaknesses* sections.

Worth noting, collusion does introduce an additional problem of *masking*, which is also just additive. For instance, DaveC attempting to perform a DDoS attack against Walter's web server (US-08) would partially mask MalloryA having gained root access to Carol's Infrastructure (US-02). Once DaveC's attack is mitigated, the masking problem resolves.

For reference:

| Usage Scenario | Description |
| --- | --- |
| US-01 | Normal Operations with Monitoring |
| US-02 | MalloryA gains root access to Carol's Infrastructure |
| US-03 | MalloryB gains root access to both Bob's and Carol's ZavaX Infrastructure |
| US-04 | MalloryC gains root access to both Alice's and Bob's ZavaX infrastructure |
| US-05 | Trudy gains root access to Walter's web server that runs the ZavaX UI |
| US-06 | DaveA attempts to perform a DDoS attack against one, two, or three RPC proxy servers |
| US-07 | DaveB attempts to perform a DDoS attack against one, two, or three ZavaX nodes |
| US-08 | DaveC attempts to perform a DDoS attack against Walter's web server |
| US-09 | Sybil attempts to cause a Zcash chain split that includes all three ZavaX nodes for one hour |
| US-10 | Red's private keys for administering the permissioned ZavaX Oracle L1 are stolen |

### Collusion Matrix

Below is a table that shows the Usage Scenarios in a matrix and indicates the consequences of collusion.

|       | US-01 | US-02 | US-03 | US-04 | US-05 | US-06 | US-07 | US-08 | US-09 | US-10 |
| ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- | ----- |
| US-01 |       | +     | +     | +     | +     | +     | +     | +     | +     | +     |
| US-02 | +     |       | +m    | +m    | +m    | +m    | +m    | +m    | +     | m     |
| US-03 | +     | m     |       | +m    | +m    | +m    | +m    | +m    | +     | m     |
| US-04 | +     | +     | +m    |       | +m    | +m    | +m    | +m    | +     | m     |
| US-05 | +     | +m    | +m    | +m    |       | +m    | +m    | +m    | +m    | m     |
| US-06 | +     | +m    | +m    | +m    | +m    |       | +m    | +m    | +     | m     |
| US-07 | +     | +m    | +m    | +m    | +m    | +     |       | +m    | +m    | m     |
| US-08 | +     | +m    | +m    | +m    | +m    | +m    | +m    |       | +m    | m     |
| US-09 | +     | +     | +     | +     | +m    | +m    | +m    | +m    |       | m     |
| US-10 | +     |       |       |       |       |       |       |       |       |       |

Key: Consequences of `row` then `column` are: `+` = additive, `·` = multiplicative, `^` = exponential, `m` = masking (does `column` mask `row`?)

## Summary

The ZavaX Oracle platform can be attacked in a number of ways, and as a three-node L1, it is especially vulnerable. One can see that all of these attacks' feasibility and impact could be mitigated, sometimes by orders of magnitude, *by just increasing the size of the network.*

Also, only under **the most severe attacks**:

- where many nodes, handling together greater than 67% of stake, are commandeered
- when Red's keys are compromised
- when Red is bribed or coerced

is it possible to actually write bad oracle data to the blockchain. In every other case, no bad data can be written, and even in these extreme cases, the blockchain can be reclaimed by its rightful operators within 24 hours due to the Zcash chain being the canonical source of truth.

A common theme of mitigating these attacks is *noticing* that the attack is taking place. Attackers can pretend to behave virtuously most of the time, making it more difficult to detect attacks, so monitoring must be continuous and vigilant.
