# Memory Integration

Use this skill when:
- governance needs context from memory-engine
- fetching active decisions/heuristics
- building task execution context

Goals:
- consume memory through ports
- avoid leaking memory-engine internals
- keep governance decoupled

Checklist:
1. What memory data is needed?
2. Is the data active/current?
3. Is the request scoped correctly?
4. Is the returned context compact?
5. Is governance still independent of memory storage details?

Never:
- couple governance domain to memory-engine schema
- bypass ports with direct DB assumptions
- embed retrieval logic into governance domain entities
