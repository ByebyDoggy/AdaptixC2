const { ethers } = require("hardhat");

async function main() {
  const address = process.env.CONTRACT_ADDRESS;
  const domain = process.env.DOMAIN;
  if (!address) throw new Error("CONTRACT_ADDRESS is required");
  if (!domain) throw new Error("DOMAIN is required");

  let enabled;
  if (process.env.ENABLED !== undefined) {
    enabled = process.env.ENABLED.toLowerCase() === "true";
  }

  const [signer] = await ethers.getSigners();
  const registry = await ethers.getContractAt("DomainRegistry", address, signer);

  const tx = enabled === undefined
    ? await registry.setDomain(domain)
    : await registry.setConfig(domain, enabled);

  console.log("tx:", tx.hash);
  await tx.wait();

  const [d, e, v, u] = await registry.getConfig();
  console.log("domain:", d, "enabled:", e, "version:", v.toString(), "updatedAt:", u.toString());
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
