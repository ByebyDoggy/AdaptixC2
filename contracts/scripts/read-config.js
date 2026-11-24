const { ethers } = require("hardhat");

async function main() {
  const address = process.env.CONTRACT_ADDRESS;
  if (!address) throw new Error("CONTRACT_ADDRESS is required");
  const registry = await ethers.getContractAt("DomainRegistry", address);
  const [domain, enabled, version, updatedAt] = await registry.getConfig();
  console.log(JSON.stringify({
    domain,
    enabled,
    version: version.toString(),
    updatedAt: updatedAt.toString(),
    owner: await registry.owner()
  }, null, 2));
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
