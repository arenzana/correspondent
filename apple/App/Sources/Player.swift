import AVFoundation
import MediaPlayer
import SwiftUI
import CorrespondentKit

/// Reads articles aloud, as the web's player (ui/src/components/Listen.svelte):
/// the audio is downloaded and kept on the device (it plays offline and in
/// the background), with lock-screen and headphone controls. An article
/// listened to the end counts as read; a queue plays a list's articles that
/// already have audio — audio is only made for an article the reader picks.
@MainActor @Observable
final class Player: NSObject, AVAudioPlayerDelegate {
    struct Item: Equatable { var id: EntryID; var title: String; var feed: String }

    private(set) var now: Item?
    private(set) var upNext: [Entry] = []
    private(set) var playing = false
    private(set) var preparing: EntryID?
    var current: TimeInterval = 0
    private(set) var duration: TimeInterval = 0
    private(set) var rate: Float = 1

    static let rates: [Float] = [1, 1.25, 1.5, 1.75, 2, 0.8]

    private var audio: AVAudioPlayer?
    private var ticker: Task<Void, Never>?
    weak var app: AppModel?

    /// How many articles' audio is kept on the device.
    static let kept = 25

    /// Kept audio, as the web's offline audio: the newest articles' files.
    static let folder: URL = {
        let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("Audio", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }()

    // MARK: Playing

    /// Reads one article aloud, making its audio if needed (the reader asked).
    func play(_ e: Entry) async {
        upNext = []
        do { try await start(e, make: true) } catch { app?.show("Couldn't read this aloud. \(error.localizedDescription)") }
    }

    /// Plays the list's unread articles that already have audio, in order.
    func playList(_ entries: [Entry], from selected: EntryID?) async {
        let start = entries.firstIndex { $0.id == selected } ?? 0
        upNext = Array(entries[start...]).filter { !$0.read || $0.id == selected }
        await next()
        if now == nil { app?.show("None of these have audio yet. Open an article and choose Listen to have it read aloud.") }
    }

    /// The next article in the queue with audio, skipping those without.
    func next() async {
        while !upNext.isEmpty {
            let e = upNext.removeFirst()
            do { try await start(e, make: false); return } catch { continue }
        }
    }

    private func start(_ e: Entry, make: Bool) async throws {
        guard let client = app?.client else { return }
        preparing = e.id
        defer { preparing = nil }
        let file = Self.folder.appendingPathComponent("\(e.id).mp3")
        if !FileManager.default.fileExists(atPath: file.path) {
            if make { try await client.makeAudio(e.id) } else if !(await client.hasAudio(e.id)) {
                throw APIError(status: 404, message: "No audio yet.")
            }
            let data = try await client.audio(e.id)
            try data.write(to: file, options: .atomic)
            trimKept()
        }
        try begin(file, item: Item(id: e.id, title: e.title, feed: app?.feedsByID[e.feedId]?.title ?? ""))
    }

    private func begin(_ file: URL, item: Item) throws {
        #if os(iOS)
        try AVAudioSession.sharedInstance().setCategory(.playback, mode: .spokenAudio)
        try AVAudioSession.sharedInstance().setActive(true)
        #endif
        audio?.stop()
        let player = try AVAudioPlayer(contentsOf: file)
        player.delegate = self
        player.enableRate = true
        player.rate = rate
        player.prepareToPlay()
        player.play()
        audio = player
        now = item
        duration = player.duration
        current = 0
        playing = true
        tick()
        publish()
        remoteCommands()
    }

    func toggle() {
        guard let audio else { return }
        if audio.isPlaying { audio.pause() } else { audio.play() }
        playing = audio.isPlaying
        publish()
    }

    func skip(_ seconds: TimeInterval) {
        guard let audio else { return }
        audio.currentTime = min(max(0, audio.currentTime + seconds), audio.duration)
        current = audio.currentTime
        publish()
    }

    func seek(_ t: TimeInterval) {
        audio?.currentTime = t
        current = t
        publish()
    }

    func nextRate() {
        rate = Self.rates[((Self.rates.firstIndex(of: rate) ?? 0) + 1) % Self.rates.count]
        audio?.rate = rate
        publish()
    }

    func stop() {
        audio?.stop()
        audio = nil
        now = nil
        upNext = []
        playing = false
        ticker?.cancel()
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
    }

    nonisolated func audioPlayerDidFinishPlaying(_ player: AVAudioPlayer, successfully flag: Bool) {
        Task { @MainActor in
            guard let done = self.now else { return }
            self.playing = false
            // Listened to the end: it counts as read.
            await self.app?.setRead([done.id], true)
            if !self.upNext.isEmpty { await self.next() }
        }
    }

    private func tick() {
        ticker?.cancel()
        ticker = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(500))
                guard let self, let a = self.audio else { return }
                self.current = a.currentTime
            }
        }
    }

    /// Keeps the 25 newest articles' audio.
    private func trimKept() {
        let fm = FileManager.default
        guard let files = try? fm.contentsOfDirectory(at: Self.folder, includingPropertiesForKeys: [.contentModificationDateKey]) else { return }
        let dated = files.map { ($0, (try? $0.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast) }
        for (url, _) in dated.sorted(by: { $0.1 > $1.1 }).dropFirst(Self.kept) { try? fm.removeItem(at: url) }
    }

    // MARK: Lock screen and headphones

    private func publish() {
        guard let now else { return }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = [
            MPMediaItemPropertyTitle: now.title,
            MPMediaItemPropertyArtist: now.feed,
            MPMediaItemPropertyAlbumTitle: "Correspondent",
            MPMediaItemPropertyPlaybackDuration: duration,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: audio?.currentTime ?? 0,
            MPNowPlayingInfoPropertyPlaybackRate: playing ? Double(rate) : 0,
        ]
    }

    private var commandsSet = false
    private func remoteCommands() {
        guard !commandsSet else { return }
        commandsSet = true
        let c = MPRemoteCommandCenter.shared()
        c.playCommand.addTarget { [weak self] _ in Task { @MainActor in if self?.playing == false { self?.toggle() } }; return .success }
        c.pauseCommand.addTarget { [weak self] _ in Task { @MainActor in if self?.playing == true { self?.toggle() } }; return .success }
        c.togglePlayPauseCommand.addTarget { [weak self] _ in Task { @MainActor in self?.toggle() }; return .success }
        c.skipBackwardCommand.preferredIntervals = [15]
        c.skipBackwardCommand.addTarget { [weak self] _ in Task { @MainActor in self?.skip(-15) }; return .success }
        c.skipForwardCommand.preferredIntervals = [30]
        c.skipForwardCommand.addTarget { [weak self] _ in Task { @MainActor in self?.skip(30) }; return .success }
        c.nextTrackCommand.addTarget { [weak self] _ in Task { @MainActor in await self?.next() }; return .success }
        c.changePlaybackPositionCommand.addTarget { [weak self] e in
            if let e = e as? MPChangePlaybackPositionCommandEvent { Task { @MainActor in self?.seek(e.positionTime) } }
            return .success
        }
    }
}

/// The player bar, as the web's: back 15, play, ahead 30, the article, the
/// next one, speed, close, and a seek line with the time.
struct PlayerBar: View {
    @Environment(Player.self) private var player
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    /// Compact: in iOS 26's tab bar accessory.
    var compact = false

    var body: some View {
        if let now = player.now {
            VStack(spacing: 4) {
                HStack(spacing: 2) {
                    Button { player.skip(-15) } label: { Image(systemName: "gobackward.15") }.iconButton().accessibilityLabel("Back 15 seconds")
                    Button { player.toggle() } label: { Image(systemName: player.playing ? "pause.fill" : "play.fill") }
                        .iconButton().foregroundStyle(theme.accent).accessibilityLabel(player.playing ? "Pause" : "Play")
                    if !compact {
                        Button { player.skip(30) } label: { Image(systemName: "goforward.30") }.iconButton().accessibilityLabel("Ahead 30 seconds")
                    }
                    Button { Task { await app.select(now.id) } } label: {
                        VStack(alignment: .leading, spacing: 1) {
                            Text(now.title).font(Typeface.ui.font(14, weight: 600)).foregroundStyle(theme.ink).lineLimit(1)
                            Text(now.feed).font(Typeface.ui.font(12)).foregroundStyle(theme.slate).lineLimit(1)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .buttonStyle(.plain)
                    if !player.upNext.isEmpty {
                        Button { Task { await player.next() } } label: { Image(systemName: "forward.end.fill") }.iconButton().accessibilityLabel("Next article")
                    }
                    if !compact {
                        Button { player.nextRate() } label: {
                            Text(String(format: player.rate == 1 ? "%.0f×" : "%g×", player.rate)).font(Typeface.ui.font(13, weight: 560)).monospacedDigit()
                                .foregroundStyle(theme.slate).frame(minWidth: 40, minHeight: 30)
                                .background(RoundedRectangle(cornerRadius: 6).stroke(theme.rule))
                        }
                        .buttonStyle(.plain).accessibilityLabel("Speed \(player.rate)")
                    }
                    Button { player.stop() } label: { Image(systemName: "xmark") }.iconButton().accessibilityLabel("Stop listening")
                }
                if !compact {
                    HStack(spacing: 8) {
                        Slider(value: Binding(get: { player.current }, set: { player.seek($0) }), in: 0...max(player.duration, 1))
                            .tint(theme.accent)
                        Text("\(clock(player.current)) / \(clock(player.duration))").font(Typeface.ui.font(12)).monospacedDigit().foregroundStyle(theme.slate)
                    }
                    .padding(.horizontal, 8)
                }
            }
            .padding(.horizontal, compact ? 6 : 8).padding(.vertical, compact ? 0 : 8)
            .background {
                if !compact {
                    RoundedRectangle(cornerRadius: 10).fill(theme.sheet)
                        .overlay(RoundedRectangle(cornerRadius: 10).stroke(theme.rule))
                        .shadow(color: .black.opacity(0.12), radius: 20, y: 8)
                }
            }
        }
    }

    private func clock(_ t: TimeInterval) -> String {
        guard t.isFinite else { return "0:00" }
        return String(format: "%d:%02d", Int(t) / 60, Int(t) % 60)
    }
}
