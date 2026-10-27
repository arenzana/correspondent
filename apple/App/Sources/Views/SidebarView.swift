import SwiftUI
import CorrespondentKit

/// The feeds pane, as the web's sidebar: the wordmark, search, the four
/// views, saved searches, folders with their feeds, and the toolbar.
struct SidebarView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    var onSearch: () -> Void
    var onSettings: () -> Void
    /// On iPhone, choosing a list moves on to it.
    var onChoose: () -> Void = {}

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Mark(size: 28)
                Text("Correspondent").font(Typeface.read.font(21, weight: 560, opsz: 48)).foregroundStyle(theme.ink)
                Spacer()
            }
            .padding(.horizontal, 18).padding(.top, 18).padding(.bottom, 14)

            Button(action: onSearch) {
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").font(.system(size: 13))
                    Text("Search or jump to…").font(Typeface.ui.font(14))
                    Spacer()
                }
                .foregroundStyle(theme.slate)
                .padding(.horizontal, 12).frame(height: 36)
                .background(RoundedRectangle(cornerRadius: 6).fill(theme.sheet))
                .overlay(RoundedRectangle(cornerRadius: 6).stroke(theme.rule))
            }
            .buttonStyle(.plain)
            .padding(.horizontal, 12).padding(.bottom, 10)

            ScrollView {
                VStack(alignment: .leading, spacing: 1) {
                    row(.unread, "Unread", "tray", app.tree.counts.unread)
                    row(.today, "Today", "sun.max", app.tree.counts.today)
                    row(.starred, "Starred", "star", app.tree.counts.starred)
                    row(.all, "All articles", "square.3.layers.3d", 0)
                    Divider().overlay(theme.rule.opacity(0.6)).padding(.vertical, 8).padding(.horizontal, 12)

                    if !app.tree.searches.isEmpty {
                        group("Searches")
                        ForEach(app.tree.searches) { s in row(.search(s.id), s.name, "magnifyingglass", s.unread) }
                        Divider().overlay(theme.rule.opacity(0.6)).padding(.vertical, 8).padding(.horizontal, 12)
                    }

                    ForEach(app.tree.folders.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }) { folder in
                        folderRow(folder)
                        if !app.collapsed.contains(folder.id) {
                            ForEach(feeds(in: folder.id)) { feedRow($0) }
                        }
                    }
                    ForEach(feeds(in: nil)) { feedRow($0) }
                }
                .padding(.horizontal, 8).padding(.bottom, 12)
            }

            HStack(spacing: 2) {
                Spacer()
                Button(action: onSettings) { Image(systemName: "slider.horizontal.3") }.iconButton()
                    .accessibilityLabel("Settings")
            }
            .padding(.horizontal, 10).padding(.vertical, 8)
            .overlay(alignment: .top) { Rectangle().fill(theme.rule.opacity(0.6)).frame(height: 1) }
        }
        .background(theme.fog)
    }

    private func feeds(in folder: Int64?) -> [Feed] {
        app.tree.feeds.filter { $0.folderId == folder }.sorted { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending }
    }

    private func group(_ label: String) -> some View {
        Text(label).font(Typeface.ui.font(12.5, weight: 600)).foregroundStyle(theme.slate)
            .padding(.horizontal, 12).padding(.top, 4).padding(.bottom, 2)
    }

    private func row(_ s: Source, _ label: String, _ icon: String, _ count: Int) -> some View {
        Button {
            Task { await app.go(s); onChoose() }
        } label: {
            HStack(spacing: 10) {
                Image(systemName: icon).font(.system(size: 14)).frame(width: 18).foregroundStyle(app.source == s ? theme.accent : theme.slate)
                Text(label).font(Typeface.ui.font(15, weight: app.source == s ? 560 : 450)).foregroundStyle(theme.ink).lineLimit(1)
                Spacer()
                Count(n: count)
            }
            .padding(.horizontal, 12).frame(height: 34)
            .background(RoundedRectangle(cornerRadius: 6).fill(app.source == s ? theme.accent.opacity(0.14) : .clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func folderRow(_ folder: Folder) -> some View {
        let open = !app.collapsed.contains(folder.id)
        let s = Source.folder(folder.id)
        return HStack(spacing: 4) {
            Button {
                if open { app.collapsed.insert(folder.id) } else { app.collapsed.remove(folder.id) }
            } label: {
                Image(systemName: "chevron.right").font(.system(size: 10, weight: .semibold))
                    .rotationEffect(.degrees(open ? 90 : 0)).foregroundStyle(theme.slate).frame(width: 18, height: 30)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(open ? "Collapse" : "Expand") \(folder.name)")
            Button {
                Task { await app.go(s); onChoose() }
            } label: {
                HStack {
                    Text(folder.name).font(Typeface.ui.font(15, weight: 520)).foregroundStyle(theme.slate).lineLimit(1)
                    Spacer()
                    Count(n: folder.unread)
                }
                .padding(.horizontal, 8).frame(height: 32)
                .background(RoundedRectangle(cornerRadius: 6).fill(app.source == s ? theme.accent.opacity(0.14) : .clear))
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        }
        .padding(.top, 6)
    }

    private func feedRow(_ feed: Feed) -> some View {
        let s = Source.feed(feed.id)
        return Button {
            Task { await app.go(s); onChoose() }
        } label: {
            HStack(spacing: 10) {
                FeedIcon(feed: feed, size: 16)
                Text(feed.title.isEmpty ? feed.url : feed.title).font(Typeface.ui.font(15))
                    .foregroundStyle(feed.paused || feed.errorCount > 0 ? theme.slate : theme.ink).lineLimit(1)
                Spacer()
                if feed.errorCount > 0 { Image(systemName: "exclamationmark.triangle").font(.system(size: 11)).foregroundStyle(theme.slate) }
                Count(n: feed.unread)
            }
            .padding(.leading, 30).padding(.trailing, 12).frame(height: 32)
            .background(RoundedRectangle(cornerRadius: 6).fill(app.source == s ? theme.accent.opacity(0.14) : .clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
