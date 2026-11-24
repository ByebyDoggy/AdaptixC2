const { expect } = require("chai");
const { ethers } = require("hardhat");

function findEvent(receipt, contract, name) {
  for (const log of receipt.logs) {
    try {
      const parsed = contract.interface.parseLog(log);
      if (parsed && parsed.name === name) return parsed;
    } catch (_) {}
  }
  return null;
}

describe("DomainRegistry", function () {
  let registry;
  let owner;
  let attacker;

  beforeEach(async function () {
    [owner, attacker] = await ethers.getSigners();
    const factory = await ethers.getContractFactory("DomainRegistry");
    registry = await factory.deploy("tcp.example.com", true);
    await registry.waitForDeployment();
  });

  it("starts with the configured domain and version 1", async function () {
    const [domain, enabled, version, updatedAt] = await registry.getConfig();
    expect(domain).to.equal("tcp.example.com");
    expect(enabled).to.equal(true);
    expect(version).to.equal(1n);
    expect(updatedAt > 0n).to.equal(true);
    expect(await registry.owner()).to.equal(owner.address);
  });

  it("owner can change the domain and event/version update", async function () {
    const tx = await registry.setDomain("c2.example.net");
    const receipt = await tx.wait();
    const event = findEvent(receipt, registry, "DomainUpdated");
    expect(event).to.not.equal(null);
    expect(event.args.domain).to.equal("c2.example.net");
    expect(event.args.enabled).to.equal(true);
    expect(event.args.version).to.equal(2n);

    const [domain, , version] = await registry.getConfig();
    expect(domain).to.equal("c2.example.net");
    expect(version).to.equal(2n);
  });

  it("owner can toggle enabled", async function () {
    await registry.setEnabled(false);
    expect(await registry.enabled()).to.equal(false);
    expect(await registry.version()).to.equal(2n);
  });

  it("owner can set domain and enabled together", async function () {
    await registry.setConfig("c2.example.org", false);
    const [domain, enabled, version] = await registry.getConfig();
    expect(domain).to.equal("c2.example.org");
    expect(enabled).to.equal(false);
    expect(version).to.equal(2n);
  });

  it("rejects non-owner writes", async function () {
    await expect(registry.connect(attacker).setDomain("evil.example")).to.be.revertedWith("not owner");
    await expect(registry.connect(attacker).setEnabled(false)).to.be.revertedWith("not owner");
    await expect(registry.connect(attacker).setConfig("evil.example", false)).to.be.revertedWith("not owner");
    await expect(registry.connect(attacker).transferOwnership(attacker.address)).to.be.revertedWith("not owner");
  });

  it("rejects invalid hosts", async function () {
    const bad = ["", "a/b", "a:b", "a b", "a\\b", "-a.com", "a.com-", "a".repeat(254)];
    for (const host of bad) {
      await expect(registry.setDomain(host)).to.be.revertedWith("invalid host");
    }
  });

  it("transfers ownership and the new owner can write", async function () {
    await registry.transferOwnership(attacker.address);
    expect(await registry.owner()).to.equal(attacker.address);
    await registry.connect(attacker).setDomain("new.example.com");
    expect(await registry.domain()).to.equal("new.example.com");
    await expect(registry.setDomain("old.example.com")).to.be.revertedWith("not owner");
  });

  it("rejects zero-address ownership transfer", async function () {
    await expect(registry.transferOwnership(ethers.ZeroAddress)).to.be.revertedWith("zero owner");
  });
});
