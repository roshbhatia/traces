package provider

#ActionName: "activity.read" | "session.current" | "session.discover" | "diff.render"

#Action: close({
	description: string & !=""
	argv?: [...string]
	env?: [=~"^[A-Za-z_][A-Za-z0-9_]*$"]: string
})

#Manifest: close({
	version: "provider/v1"
	name: string & =~"^[a-z][a-z0-9._-]*$"
	description: string & !=""
	command: [string & !="", ...string]
	actions: [#ActionName]: #Action
	requires?: close({
		commands?: [...string & !=""]
		environment?: [...string & =~"^[A-Za-z_][A-Za-z0-9_]*$"]
		paths?: [...string & !=""]
	})
	defaults?: close({
		timeout?: string & =~"^[0-9]+(ns|us|µs|ms|s|m|h)$"
		priority?: int
	})
})
