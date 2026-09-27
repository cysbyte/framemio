import AVFoundation
import AVKit
import SwiftUI

private func storyVideoPlayerDebugLog(_ message: @autoclosure () -> String) {
#if DEBUG
    print("[FrameMio][StoryVideoPlayer] \(message())")
#endif
}

/// Plays a finished film inline, with the system transport controls so the
/// viewer can pause, scrub, or go fullscreen.
///
/// The URL is a presigned S3 link that expires, so the player is rebuilt when it
/// changes rather than holding a stale item.
struct StoryVideoPlayer: View {
    let url: URL
    let autoplays: Bool
    let isPlaybackEnabled: Bool
    let posterImage: Image?
    let accessibilityTitle: String

    @StateObject private var controller: StoryVideoPlayerController

    init(
        url: URL,
        autoplays: Bool = true,
        isPlaybackEnabled: Bool = true,
        posterImage: Image? = nil,
        accessibilityTitle: String = "Your film"
    ) {
        self.url = url
        self.autoplays = autoplays
        self.isPlaybackEnabled = isPlaybackEnabled
        self.posterImage = posterImage
        self.accessibilityTitle = accessibilityTitle
        _controller = StateObject(wrappedValue: StoryVideoPlayerController(url: url))
    }

    var body: some View {
        ZStack {
            VideoPlayer(player: controller.player)

            if let posterImage, !controller.isReady {
                posterImage
                    .resizable()
                    .scaledToFill()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .clipped()
                    .allowsHitTesting(false)
            }
        }
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
    @Published private(set) var isReady = false

    private var currentURL: URL?
    private var endObserver: NSObjectProtocol?
    private var playerStatusObserver: NSKeyValueObservation?
    private var timeControlObserver: NSKeyValueObservation?
    private var currentItemStatusObserver: NSKeyValueObservation?
    private var currentItemErrorObserver: NSKeyValueObservation?
    private var currentItemBufferObserver: NSKeyValueObservation?
    private var currentItemKeepUpObserver: NSKeyValueObservation?
    private var itemFailureObserver: NSObjectProtocol?

    init(url: URL) {
        attachPlayerObservers()
        load(url: url)
    }

    func start(autoplays: Bool) {
        // Playback is the whole point of this screen, so the film should not be
        // silenced by an unrelated ringer switch.
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
        try? AVAudioSession.sharedInstance().setActive(true)
        storyVideoPlayerDebugLog("playback enabled current_url=\(Self.redactedURL(currentURL)) autoplays=\(autoplays) player_status=\(String(describing: player.status)) time_control=\(String(describing: player.timeControlStatus))")
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
        storyVideoPlayerDebugLog("playback paused current_url=\(Self.redactedURL(currentURL)) time_control=\(String(describing: player.timeControlStatus))")
        player.pause()
    }

    func replace(url: URL, autoplays: Bool) {
        guard url != currentURL else { return }
        storyVideoPlayerDebugLog("replacing player item old_url=\(Self.redactedURL(currentURL)) new_url=\(Self.redactedURL(url)) autoplays=\(autoplays)")
        load(url: url)
        if autoplays {
            player.play()
        }
    }

    private func load(url: URL) {
        currentURL = url
        isReady = false
        let urlDescription = Self.redactedURL(url)
        if let endObserver {
            NotificationCenter.default.removeObserver(endObserver)
            self.endObserver = nil
        }
        if let itemFailureObserver {
            NotificationCenter.default.removeObserver(itemFailureObserver)
            self.itemFailureObserver = nil
        }

        player.replaceCurrentItem(with: nil)

        let item = AVPlayerItem(url: url)
        attachItemObservers(item, urlDescription: urlDescription)
        storyVideoPlayerDebugLog("loading player item url=\(urlDescription)")
        Task {
            do {
                let duration = try await item.asset.load(.duration)
                let durationSeconds = duration.seconds
                let durationDescription = durationSeconds.isFinite ? String(format: "%.3f", durationSeconds) : "unknown"
                storyVideoPlayerDebugLog("loaded player item duration url=\(urlDescription) asset_duration=\(durationDescription)")
            } catch {
                storyVideoPlayerDebugLog("loaded player item duration failed url=\(urlDescription) error=\(error.localizedDescription)")
            }
        }
        player.replaceCurrentItem(with: item)

        // Leave the last frame up and rewind, so replay is one tap rather than a
        // blank player.
        endObserver = NotificationCenter.default.addObserver(
            forName: .AVPlayerItemDidPlayToEndTime,
            object: item,
            queue: .main
        ) { [weak player] _ in
            storyVideoPlayerDebugLog("item reached end current_url=\(urlDescription)")
            player?.seek(to: .zero)
        }
    }

    private func attachPlayerObservers() {
        playerStatusObserver = player.observe(\.status, options: [.initial, .new]) { player, _ in
            storyVideoPlayerDebugLog("player status changed status=\(String(describing: player.status)) current_item_status=\(String(describing: player.currentItem?.status)) error=\(player.error?.localizedDescription ?? "nil")")
        }
        timeControlObserver = player.observe(\.timeControlStatus, options: [.initial, .new]) { player, _ in
            storyVideoPlayerDebugLog("player time control changed status=\(String(describing: player.timeControlStatus)) reason=\(player.reasonForWaitingToPlay.map(String.init(describing:)) ?? "nil") rate=\(player.rate)")
        }
    }

    private func attachItemObservers(_ item: AVPlayerItem, urlDescription: String) {
        currentItemStatusObserver = item.observe(\.status, options: [.initial, .new]) { item, _ in
            let durationSeconds = item.duration.seconds
            let durationDescription = durationSeconds.isFinite ? String(format: "%.3f", durationSeconds) : "unknown"
            storyVideoPlayerDebugLog("item status changed url=\(urlDescription) status=\(String(describing: item.status)) duration=\(durationDescription) error=\(item.error?.localizedDescription ?? "nil")")
            Task { @MainActor [weak self] in
                self?.isReady = item.status == .readyToPlay
            }
        }
        currentItemErrorObserver = item.observe(\.error, options: [.initial, .new]) { item, _ in
            if let error = item.error {
                storyVideoPlayerDebugLog("item error changed url=\(urlDescription) error=\(error.localizedDescription)")
            }
        }
        currentItemBufferObserver = item.observe(\.isPlaybackBufferEmpty, options: [.initial, .new]) { item, _ in
            storyVideoPlayerDebugLog("item buffer state url=\(urlDescription) empty=\(item.isPlaybackBufferEmpty) likelyToKeepUp=\(item.isPlaybackLikelyToKeepUp)")
        }
        currentItemKeepUpObserver = item.observe(\.isPlaybackLikelyToKeepUp, options: [.initial, .new]) { item, _ in
            storyVideoPlayerDebugLog("item likely to keep up changed url=\(urlDescription) likely=\(item.isPlaybackLikelyToKeepUp) bufferEmpty=\(item.isPlaybackBufferEmpty)")
        }
        itemFailureObserver = NotificationCenter.default.addObserver(
            forName: .AVPlayerItemFailedToPlayToEndTime,
            object: item,
            queue: .main
        ) { notification in
            let error = (notification.userInfo?[AVPlayerItemFailedToPlayToEndTimeErrorKey] as? NSError)?.localizedDescription ?? item.error?.localizedDescription ?? "unknown"
            storyVideoPlayerDebugLog("item failed to play to end current_url=\(urlDescription) error=\(error)")
        }
    }

    nonisolated private static func redactedURL(_ url: URL?) -> String {
        guard let url else { return "nil" }
        let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        let host = components?.host ?? url.host ?? "nil"
        let path = components?.percentEncodedPath ?? url.path
        let lastComponent = URL(fileURLWithPath: path).lastPathComponent
        return "\(host)/\(lastComponent)"
    }

    deinit {
        if let endObserver {
            NotificationCenter.default.removeObserver(endObserver)
        }
        if let itemFailureObserver {
            NotificationCenter.default.removeObserver(itemFailureObserver)
        }
    }
}
