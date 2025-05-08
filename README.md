# Moca CometBFT

Moca CometBFT, forked from [CometBFT](https://github.com/cometbft/cometbft),
is the consensus layer of Moca blockchain.
CometBFT is a Byzantine Fault Tolerant (BFT) middleware that takes a
state transition machine - written in any programming language - and securely
replicates it on many machines.

For protocol details, refer to the [CometBFT Specification](./spec/README.md).

For detailed analysis of the consensus protocol, including safety and liveness
proofs, read our paper, "[The latest gossip on BFT
consensus](https://arxiv.org/abs/1807.04938)".

## Disclaimer

**The software and related documentation are under active development, all subject to potential future change without
notification and not ready for production use. The code and security audit have not been fully completed and not ready
for any bug bounty. We advise you to be careful and experiment on the network at your own risk. Stay safe out there.**

## Key features

We implement several key features based on the CometBFT fork:

* Vote Pool. Vote pool is used to collect votes from different validators for off-chain consensus.
Currently, it is mainly used for cross chain and data availability challenge in Moca blockchain.
* RANDAO. RANDAO is introduced for on-chain randomness. Overall, the idea is very similar to the RANDAO
in Ethereum beacon chain, you can refer to [here](https://eth2book.info/altair/part2/building_blocks/randomness)
for more information. It has some limitations, please use it with caution.

## Minimum requirements

| Requirement | Notes             |
| ----------- | ----------------- |
| Go version  | Go 1.20 or higher |

### Install

See the [install guide](./docs/guides/install.md).

### Quick Start

* [Single node](./docs/guides/quick-start.md)
* [Local cluster using docker-compose](./docs/networks/docker-compose.md)

## Contributing

Please abide by the [Code of Conduct](CODE_OF_CONDUCT.md) in all interactions.

Before contributing to the project, please take a look at the [contributing
guidelines](CONTRIBUTING.md) and the [style guide](STYLE_GUIDE.md). You may also
find it helpful to read the [specifications](./spec/README.md), and familiarize
yourself with our [Architectural Decision Records
(ADRs)](./docs/architecture/README.md) and [Request For Comments (RFCs)](./docs/rfc/README.md).

## Resources

### Libraries

* [Cosmos SDK](http://github.com/cosmos/cosmos-sdk); A framework for building
  applications in Golang
* [Tendermint in Rust](https://github.com/informalsystems/tendermint-rs)
* [ABCI Tower](https://github.com/penumbra-zone/tower-abci)

### Applications

* [Cosmos Hub](https://hub.cosmos.network/)
* [Terra](https://www.terra.money/)
* [Celestia](https://celestia.org/)
* [Anoma](https://anoma.network/)
* [Vocdoni](https://docs.vocdoni.io/)

### Research

* [The latest gossip on BFT consensus](https://arxiv.org/abs/1807.04938)
* [Master's Thesis on Tendermint](https://atrium.lib.uoguelph.ca/xmlui/handle/10214/9769)
* [Original Whitepaper: "Tendermint: Consensus Without Mining"](https://tendermint.com/static/docs/tendermint.pdf)

## License

The Moca CometBFT library (i.e. all code outside the `cmd` directory) is licensed under the
[GNU Lesser General Public License v3.0](https://www.gnu.org/licenses/lgpl-3.0.en.html),
also included in our repository in the `COPYING.LESSER` file.

The Moca CometBFT binaries (i.e. all code inside the `cmd` directory) is licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.en.html), also
included in our repository in the `COPYING` file.

## Fork Information

This project is forked from [greenfield-cometbft](https://github.com/bnb-chain/greenfield-cometbft). Significant changes have been made to adapt the project for specific use cases, but much of the core functionality comes from the original project.
