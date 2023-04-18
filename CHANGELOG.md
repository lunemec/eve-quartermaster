# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
## [1.1.10] - 2023-04-18
- Updated bot to never respond to DMs.
## [1.1.9] - 2022-11-22
- Changed "Doctrine ships stock low" -> "Doctrine ships contracts low".
## [1.1.8] - 2022-05-18
- Fixed bug in the N historical prices checking, it is now correct.
## [1.1.7] - 2022-05-16
- Prices of doctrines are now based on historical price, using MAX() of last N prices,
  where N is 2x required number to be stocked. This makes sure any stocked ships get
  sold for original price, while newly accepted cheaper or more expensive will eventually
  catch up to the buy price.
## [1.1.6] - 2022-04-12
- Fixed bug in `!leaderboard` showing `#System` from IssuerID 0.
## [1.1.5] - 2022-04-05
- `!leaderboard` can now be called with time-range to show custom leaderboards.
- Added `!migrate FROM TO` command to easily change versions of doctrines.
- Fixed bug when deleting unused doctrines left them there.
## [1.1.4] - 2022-03-30
- Fixed bug with tracking prices when there is unknown doctrine present.
## [1.1.3] - 2022-03-23
- Doctrine price will reset every 3 months.
## [1.1.2] - 2022-03-23
- Add current month and year to the leaderboard title.
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
