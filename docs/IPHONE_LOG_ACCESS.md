# iPhone Log Access

This guide explains how to inspect FrameMio logs from a physical iPhone when testing features such as StoreKit / in-app purchases.

## Quick Xcode Console Flow

Use this when you already launched the app from Xcode.

1. Connect the iPhone by USB, unlock it, and tap **Trust This Computer** if prompted.
2. In Xcode, select the physical iPhone as the run destination.
3. Click **Run**.
4. Open the debug console at the bottom of Xcode.
5. Use the console filter field and search for:
   - `FrameMio`
   - `[FrameMio][Pricing]`
   - `StoreKit`
   - `AMSError`
   - `ASDError`
6. Reproduce the issue in the app, then copy the relevant log lines.

For IAP issues, the most useful app logs are:

```text
[FrameMio][Pricing] load_products_requested ...
[FrameMio][Pricing] load_products_loaded ...
[FrameMio][Pricing] purchase_missing_product ...
```

## Device Discovery From Terminal

Check that macOS command-line tools can see the iPhone:

```sh
xcrun devicectl list devices
xcrun xcdevice list
```

Expected result: the physical iPhone appears with platform `com.apple.platform.iphoneos` and `available = true`.

If only `My Mac` or simulators appear:

1. Unlock the iPhone.
2. Reconnect USB.
3. Confirm the trust prompt on the iPhone.
4. Make sure Xcode can see the device.
5. Retry the commands.

## Recent macOS Unified Logs

Use `log show` to inspect recent logs captured by Xcode and related Apple services:

```sh
/usr/bin/log show --last 10m \
  --predicate 'process == "FrameMio" OR eventMessage CONTAINS[c] "FrameMio" OR eventMessage CONTAINS[c] "StoreKit" OR eventMessage CONTAINS[c] "AMSError" OR eventMessage CONTAINS[c] "ASDError"' \
  --style compact
```

Useful filters:

```sh
/usr/bin/log show --last 10m --predicate 'eventMessage CONTAINS[c] "[FrameMio][Pricing]"' --style compact
/usr/bin/log show --last 10m --predicate 'eventMessage CONTAINS[c] "Finance Authentication Error"' --style compact
```

## Xcode Run Result Logs

Xcode stores run logs in DerivedData. For FrameMio, the path usually looks like:

```text
~/Library/Developer/Xcode/DerivedData/FrameMio-*/Logs/Launch/Run-FrameMio-*.xcresult
```

List recent run results:

```sh
ls -td ~/Library/Developer/Xcode/DerivedData/FrameMio-*/Logs/Launch/Run-FrameMio-*.xcresult | head
```

Extract the latest console log reference:

```sh
latest=$(ls -td ~/Library/Developer/Xcode/DerivedData/FrameMio-*/Logs/Launch/Run-FrameMio-*.xcresult | head -n 1)
ref=$(xcrun xcresulttool get --path "$latest" --format json \
  | plutil -extract actions._values.0.actionResult.consoleLogRef.id._value raw -o - -)
echo "$latest"
echo "$ref"
```

Filter the console log for FrameMio / StoreKit messages:

```sh
xcrun xcresulttool get --path "$latest" --id "$ref" --format json \
  | rg "\\[FrameMio\\]\\[Pricing\\]|StoreKit|AMSError|ASDError|Finance Authentication|load_products|purchase_missing|purchase_tapped"
```

## StoreKit / IAP Log Checklist

When debugging subscription products, confirm these values in the logs:

- `proxyAppBundleID=com.framemio.app`
- `client-environment-type=Sandbox`
- `storefront-country-code=...`
- Requested product IDs match the app constants:
  - `com.framemio.pro.monthly`
  - `com.framemio.pro.yearly`
- Returned products include the selected product.

If logs show `Finance Authentication Error` or `AMSStatusCode=401`, reset the sandbox account on the iPhone:

1. Open **Settings → Developer → Sandbox Apple Account**.
2. Sign out.
3. Restart the iPhone.
4. Sign in again with a valid App Store Connect sandbox tester.
5. Re-run the app from Xcode and test again.

Also verify App Store Connect configuration:

- The app bundle ID is `com.framemio.app`.
- The app and IAP products belong to the same Apple Developer Team used by Xcode signing.
- Product IDs exactly match the app constants.
- Subscription product has price, availability, localization, and subscription group configured.
- The product is available in the logged storefront country.

## Common Problems

### Xcode Shows Logs But Terminal Does Not See iPhone

Run terminal commands with enough permissions, reconnect USB, and keep the iPhone unlocked. Xcode may have a live debug session even when `devicectl` initially reports no devices.

### StoreKit Products Do Not Load

Look for:

```text
[FrameMio][Pricing] load_products_loaded monthly=false yearly=false
```

This usually means App Store Connect did not return products for the requested identifiers. Check product IDs, signing team, product metadata, storefront availability, and sandbox propagation delay.

### Purchase Button Shows Missing Product

Look for:

```text
[FrameMio][Pricing] purchase_missing_product selected=monthly product_id=com.framemio.pro.monthly ...
```

This means the selected `Product` is still `nil`; the app should not attempt a StoreKit purchase until products load.
