# Deepgram setup for iOS

FrameMio reads Deepgram config in this order:

1. `DEEPGRAM_API_KEY` / `DEEPGRAM_LANGUAGE` from the Xcode run environment
2. `ios/FrameMio.env` bundled into the app
3. `ios/FrameMio/Info.plist`

For local development, edit `ios/FrameMio.env` and set:

- `DEEPGRAM_API_KEY`
- `DEEPGRAM_LANGUAGE` (optional)

Example:

```env
DEEPGRAM_API_KEY=your_real_deepgram_key_here
DEEPGRAM_LANGUAGE=en
```

The app logs which source it used at startup:

- `[FrameMio][Config] deepgram_api_key_configured=true deepgram_language=en source=local_env`

If the log says `false`, the app will fall back to the backend token endpoint.
