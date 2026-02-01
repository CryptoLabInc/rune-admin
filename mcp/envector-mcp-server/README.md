# enVector-MCP-Server
## Description
We provide MCP Server of `enVector`, `CryptoLab, Inc.`'s HE (Homomorphic Encryption)-Based vector search engine.

## What is MCP?
[MCP](https://modelcontextprotocol.io/docs/getting-started/intro), which stands for `Model Context Protocol`, is a protocol used by AI application for access to the following services:
1) External data
2) Tools
3) Workflow

It is kind of pre-defined JSON format protocol.

### Participant in MCP
There are 3 participant in `MCP` communication.
- Host
    + AI application
    + ex. `VS Code`, `Claude`, and so on
- Client
    + Connection module of Host
    + Form of expansion or add-on module (1:1 for each server)
- Server
    + Supplier of Data/Tools
    + In our case, `enVector`

## How `enVector-MCP-Server` can be implemented to services?
As `enVector` is vector search engine based on HE, this `enVector MCP Server` can be used in some cases like below.

### Use Scenario
- AI chat-bot emplaced in private network (IntraNet / Air-Gapped Net)
    + This chat-bot need to use secured dataset.

    In this case,

    1. To get data, AI send query to `enVector` via `enVector MCP Server`.
    2. `enVector` do vector search and returns encryptred scoreboard.
    3. Then AI decrypt scoreboard and require most appropriate vector's metadata to secured DB.
    4. After get response, AI decrypt dataset and show it to user.

- SW Developer, who is affiliated at some SW Dev Team, is running new project.
    + They wanna refer to their previous project to reuse some codes.
    + They are using code assistant module
    + Their previous project is under protection with private repository.

    In this case,

    1. Code assistant generate new skeleton codes first and then, try to search similar codes in DB.
    2. As codes are protected as encrypted form, code assistant AI call `enVector MCP Server` to search code candidates via `enVector`.
    3. Then, `enVector` returns scoreboard of codes stored in secured DB.
    4. Code Assistant AI now can require top-k code blocks stored in specific index in DB.
    5. After decrypting returned code blocks, code assistant AI can improve skeleton codes with them.

- Assumption
    + `enVector` can run on anywhere
    + Each terminal(devices) user just need to add `enVector MCP Server` on their AI assistant (or else).

- Expectation

    With pre-defined protocol that `enVector MCP Server` uses, all 'secured-data search' will be processed automatically.

## Languages Support

- Python3
