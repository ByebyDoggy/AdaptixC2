# DomainRegistry (AdaptixC2 C2 domain control plane)

Owner-controlled host registry. The contract stores **host only** — no scheme,
path, port, or token. Agents read `domain()` / `enabled()` via `eth_call` and
rewrite the host of their embedded `wss://` URL.

## Test

```bash
npm install
npx hardhat test
```

## BSC testnet deploy

Set the owner key only in the local environment (never commit it):

```bash
export BSC_RPC_URL="https://data-seed-prebsc-1-s1.bnbchain.org:8545"
export BSC_OWNER_KEY="0x..."          # testnet owner key, local env only
export DOMAIN="tcp.sweeper.channel"
export ENABLED="true"
npx hardhat run scripts/deploy.js --network bscTestnet
```

The deploy script prints the contract address. Fund the owner address with
tBNB from https://www.bnbchain.org/en/testnet-faucet.

## Change the on-chain domain

```bash
export CONTRACT_ADDRESS="0x..."
export DOMAIN="tcp2.sweeper.channel"
export ENABLED="true"
npx hardhat run scripts/set-domain.js --network bscTestnet
```

Without `ENABLED`, only `setDomain` is called. With `ENABLED`, `setConfig`
updates both fields.

## Read current config

```bash
export CONTRACT_ADDRESS="0x..."
npx hardhat run scripts/read-config.js --network bscTestnet
```

## BSC testnet deployment (current)

- Contract: `0x63cd2A5cB365170bB852f3A7864383800B62DAe5`
- Chain ID: `97`
- RPC: `https://data-seed-prebsc-1-s1.bnbchain.org:8545`
- Initial domain: `tcp.sweeper.channel`
- `enabled`: `true`
- Owner: `0x400Dd6B76931c2Ca76745729d31d7142fBDC1512`

Teamserver build config lives in `data/chain.json`; operators can edit it in
the client under **Settings → C2 Domain / Chain**, refresh the on-chain status,
and copy the `setDomain(string)` calldata for an external owner wallet.

## Test wallet

Owner wallet used for the BSC testnet prototype:
`0x400Dd6B76931c2Ca76745729d31d7142fBDC1512`

The private key is supplied via `BSC_OWNER_KEY` at runtime and must never be
written to this repository, the teamserver config, or logs.
