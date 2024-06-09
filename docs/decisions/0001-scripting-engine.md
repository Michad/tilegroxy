---
status: "proposed"
date: 2024-06-XX
---
# Scripting Engine for Custom Providers and Auth

## Context and Problem Statement

We want the ability to provide custom providers that don't live inside this main repository. This allows groups that utilize this project to implement tile layers that involve very domain specific logic. For example, many companies provide map layers as part of their product offering that is integrated behind their own authentication methodology, often this doesn't follow any standard scheme. 

Additionally, we'd like to be able to allow the same system for custom authentication solutions and maybe caches down the road.

## Decision Drivers

* Developer experience
* Ease to invoke from the main application
* Maturity and stability of the engine
* Reasonably low additional latency for a typical invocation
* Safety to call in parallel from multiple threads

## Considered Options

* Go - precompilation only
* Go - scripting solution
* Lua - scripting solution
* Python - scripting solution
* Javascript - scripting solution

## Decision Outcome

Chosen option: "{title of option 1}", because
{justification. e.g., only option, which meets k.o. criterion decision driver | which resolves force {force} | … | comes out best (see below)}.

## Pros and Cons of the Options

### Go - precompilation only 

The null option, don't support anything beyond the core interface used for built-in providers.  Instead focus on making it as easy as possible to build on this software so if a group needs to add a custom provider, they write it as a native provider either in their own fork of this repo or in their own project that pulls this in as a dependency.

* Pro: Easiest option, no extra coding required
* Con: Users of this software need to maintain their own forks and build processes
* Con: Changes to the core provider interface will break things for users
* Con: Go is less accessible than other options

### Go - scripting solution

Utilize [Yaegi](https://github.com/traefik/yaegi) to allow custom providers to be written in Go but interpreted at runtime.

* Pro: follows the pattern of traefik, which is a well known and similar tool
* Con: Go is less accessible than other options
* Con: 

### Lua - scripting solution

Utilize either [go-lua](https://github.com/Shopify/go-lua) or [gopher-lua](https://github.com/yuin/gopher-lua) to provide Lua scripting.

* Pro: Lua is very popular for providing scripting/plugin functionality
* Con: Specific to gopher-lua: it's mature but not very well maintained

### Python - scripting solution

Utilize a library that helps Go be able to call Python. This would require separate executables.

* Pro: Very well-known language
* Pro: Can provide easiest transition path for custom providers written for tilestache
* Neutral: That easy transition path makes it more difficult to change the interface since tilegroxy isn't a tilestache port
* Bad: Python can be environmentally temperamental. Requiring cpython complicates installation/container maintenance
* Bad: Tools to support go/python interop mostly either aren't mature or aren't well maintained


### Javascript - scripting solution

Allow custom providers to be written in javascript. This can either be via an interpreter written in Go such as [otto](https://github.com/robertkrimen/otto) or a v8 binding such as [v8go](https://github.com/rogchap/v8go).

* Pro: Javascript is currently probably the most universal language 
* 

## More Information

It's possible multiple options could be offered. However the maintenance cost of that makes it particularly unappealing.  

The main driver of this decision is from proving out a sample implementation for each option that simply reads a static file from the file system and returns that as the tile.
