# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
## [1.1.1] - 2022-03-23
- Fix leaderboard ordering.
- Show only top 10 in leaderboard.
## [1.1.0] - 2022-03-22
- Changed repository backend from JSON file to bbolt key value storage.
- Added the ability to track prices of hauled-in contracts if they start with * (asterisk).
- Added `!price fetch` command to force reloading from API to check 
  for price-tracking contracts starting with * and saving the data.
- Added `!price set` command to set doctrine price to a certain value.
  This does not make record in price_history, just sets the doctrine value
  to this, so it can be alerted.
- Added alerting for doctrine contracts at lower price than it was bought for on
  `!report full` command.
- Added `!leaderboard` command which tracks top 10 haulers who made the most
  price-tracking contracts.
- Fixed some token error propagation.
## [1.0.3] - 2021-11-12
- Fixed issue with long messages not sending to Discord.
- Changed message formats to be consistent.
## [1.0.2] - 2021-10-28
- Upgraded SSO to V2 and goesi.
## [1.0.1] - 2021-10-15
- Added alert about problematic contracts to "!report full" command.
    - Problematic contracts are:
      - Expired
      - Not Item Exchange (auction for example)
      - Bad state
## [1.0.0] - 2021-10-11
- Version 1.0.0, many features added and polished while in use.
## [0.0.1] - 2021-09-18
- First version of quartermaster bot, copied from eve-fuelbot.
