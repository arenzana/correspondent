import SwiftUI
import CorrespondentKit

/// Search, as the web's command palette: feeds and lists by name, and
/// articles by their text as you type.
struct SearchView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss
    @State private var text = ""
    @State private var found: [Entry] = []
    @FocusState private var focused: Bool
    var onOpen: (EntryID) -> Void = { _ in }
    var onList: () -> Void = {}
    /// In the iPhone's Search tab rather than a sheet: nothing to dismiss.
    var inTab = false

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "magnifyingglass").foregroundStyle(theme.slate)
                TextField("Search articles, or jump to a feed", text: $text)
                    .font(Typeface.ui.font(17)).foregroundStyle(theme.ink).textFieldStyle(.plain)
                    .focused($focused).autocorrectionDisabled()
                    .onSubmit { allMatching() }
                if !inTab {
                    Button("Done") { dismiss() }.font(Typeface.ui.font(15, weight: 560)).tint(theme.accent)
                }
            }
            .padding(.horizontal, 16).frame(height: 56)
            .overlay(alignment: .bottom) { Rectangle().fill(theme.rule).frame(height: 1) }

            List {
                let term = text.trimmingCharacters(in: .whitespaces)
                let feeds = term.isEmpty ? [] : app.tree.feeds.filter { $0.title.localizedCaseInsensitiveContains(term) }.prefix(5)
                if !feeds.isEmpty {
                    Section("Feeds") {
                        ForEach(Array(feeds)) { f in
                            Button { Task { await app.go(.feed(f.id)) }; onList(); dismiss() } label: {
                                HStack(spacing: 10) { FeedIcon(feed: f, size: 16); Text(f.title).font(Typeface.ui.font(15)).foregroundStyle(theme.ink) }
                            }
                        }
                    }
                }
                if !found.isEmpty {
                    Section("Articles") {
                        ForEach(found) { e in
                            Button { Task { await app.search(term); await app.select(e.id) }; onOpen(e.id); dismiss() } label: {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(e.title).font(Typeface.read.font(16, weight: 560)).foregroundStyle(theme.ink).lineLimit(2)
                                    Text(app.feedsByID[e.feedId]?.title ?? "").font(Typeface.ui.font(12.5)).foregroundStyle(theme.slate)
                                }
                            }
                        }
                    }
                }
                if !term.isEmpty {
                    Button { allMatching() } label: {
                        Label("All articles matching “\(term)”", systemImage: "magnifyingglass").font(Typeface.ui.font(15)).foregroundStyle(theme.accent)
                    }
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
        }
        .background(theme.sheet)
        .onAppear { focused = true }
        .task(id: text) {
            let term = text.trimmingCharacters(in: .whitespaces)
            guard term.count >= 2, let client = app.client else { found = []; return }
            try? await Task.sleep(for: .milliseconds(180))
            if Task.isCancelled { return }
            found = (try? await client.entries(.init(search: term, limit: 8)).entries) ?? []
        }
    }

    private func allMatching() {
        let term = text.trimmingCharacters(in: .whitespaces)
        guard !term.isEmpty else { return }
        Task { await app.search(term) }
        onList()
        dismiss()
    }
}
