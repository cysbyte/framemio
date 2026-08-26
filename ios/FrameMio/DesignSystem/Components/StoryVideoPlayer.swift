import AVFoundation
import AVKit
import SwiftUI

/// Plays a finished film inline, with the system transport controls so the
/// viewer can pause, scrub, or go fullscreen.
///
/// The URL is a presigned S3 link that expires, so the player is rebuilt when it
/// changes rather than holding a stale item.
struct StoryVideoPlayer: View {
    let url: URL
    let autoplays: Bool
    let isPlaybackEnabled: Bool
    let accessibilityTitle: String

    @StateObject private var controller: StoryVideoPlayerController

    init(
        url: URL,
        autoplays: Bool = true,
        isPlaybackEnabled: Bool = true,
        accessibilityTitle: String = "Your film"
    ) {
        self.url = url
        self.autoplays = autoplays
        self.isPlaybackEnabled = isPlaybackEnabled
        self.accessibilityTitle = accessibilityTitle
        _controller = StateObject(wrappedValue: StoryVideoPlayerController(url: url))
    }

    var body: some View {
        VideoPlayer(player: controller.player)
            .onAppear { controller.setPlaybackEnabled(isPlaybackEnabled, autoplays: autoplays) }
            .onDisappear { controller.pause() }
            .onChange(of: url) { _, newURL in
                controller.replace(url: newURL, autoplays: autoplays && isPlaybackEnabled)
            }
            .onChange(of: isPlaybackEnabled) { _, enabled in
                controller.setPlaybackEnabled(enabled, autoplays: autoplays)
            }
            .accessibilityLabel(accessibilityTitle)
    }
}

@MainActor
final class StoryVideoPlayerController: ObservableObject {
    let player = AVPlayer()

    private var currentURL: URL?
    private var endObserver: NSObjectProtocol?

    init(url: URL) {
        load(url: url)
    }

    func start(autoplays: Bool) {
        // Playback is the whole point of this screen, so the film should not be
        // silenced by an unrelated ringer switch.
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
        try? AVAudioSession.sharedInstance().setActive(true)
        if autoplays {
            player.play()
        }
    }

    func setPlaybackEnabled(_ isEnabled: Bool, autoplays: Bool) {
        if isEnabled {
            start(autoplays: autoplays)
        } else {
            pause()
        }
    }

    func pause() {
        player.pause()
    }

    func replace(url: URL, autoplays: Bool) {
        guard url != currentURL else { return }
        load(url: url)
        if autoplays {
            player.play()
        }
    }

    private func load(url: URL) {
        currentURL = url
        if let endObserver {
            NotificationCenter.default.removeObserver(endObserver)
        }
        let item = AVPlayerItem(url: url)
        player.replaceCurrentItem(with: item)
        // Leave the last frame up and rewind, so replay is one tap rather than a
        // blank player.
        endObserver = NotificationCenter.default.addObserver(
            forName: .AVPlayerItemDidPlayToEndTime,
            object: item,
            queue: .main
        ) { [weak player] _ in
            player?.seek(to: .zero)
        }
    }

    deinit {
        if let endObserver {
            NotificationCenter.default.removeObserver(endObserver)
        }
    }
}
