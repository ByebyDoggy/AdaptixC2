const { ethers } = require("hardhat");

async function main() {
  const domain = process.env.DOMAIN || "tcp.sweeper.channel";
  const enabled = (process.env.ENABLED || "true").toLowerCase() !== "false";

  const factory = await ethers.getContractFactory("DomainRegistry");
  const registry = await factory.deploy(domain, enabled);
  await registry.waitForDeployment();

  console.log("DomainRegistry:", await registry.getAddress());
  console.log("owner:", await registry.owner());
  console.log("domain:", await registry.domain());
  console.log("enabled:", await registry.enabled());
  console.log("version:", (await registry.version()).toString());
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
