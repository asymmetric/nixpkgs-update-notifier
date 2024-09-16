> [!WARNING]
> Status: alpha

# nixpkgs-update-notifier

A Matrix bot that allows you to subscribe to build failures for [nixpkgs-update](https://nix-community.github.io/nixpkgs-update/) (aka [`r-ryantm`](https://github.com/r-ryantm)).

Message the bot and type `help` to see a list of commands.

## Limitations

The bot has no access to the actual exit code of the nixpkgs-update runners, so it simply looks for the presence of the word `error` in the logs.
This is obviously fallible, and can lead to false positives (e.g. [here](https://nixpkgs-update-logs.nix-community.org/testlib/2024-09-13.log)) and false negatives (feel free to report them).
