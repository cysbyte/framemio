import SwiftUI
import UIKit

struct MediaPickerView: View {
    @Environment(\.openURL) private var openURL
    @ObservedObject private var viewModel: MediaPickerViewModel
    let onContinue: ([MediaAsset]) -> Void

    init(viewModel: MediaPickerViewModel, onContinue: @escaping ([MediaAsset]) -> Void) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
        self.onContinue = onContinue
    }

    var body: some View {
        Group {
            if viewModel.isAccessBlocked {
                accessState
            } else if viewModel.isLoading && viewModel.totalAssetCount == 0 {
                LoadingState(
                    title: "Finding your moments",
                    message: "Looking through the time and place around \(viewModel.context.rawValue.lowercased())."
                )
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if viewModel.totalAssetCount == 0 {
                EmptyState(
                    systemImage: "photo.on.rectangle.angled",
                    title: "No moments found",
                    message: "Try another starting point or give FrameMio access to your photo library.",
                    actionTitle: "Try Again",
                    action: { viewModel.load() }
                )
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                mediaBrowser
            }
        }
        .background(FrameMioColors.canvas)
        .navigationTitle(viewModel.context.pickerTitle)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Text("\(viewModel.selectedCount)")
                    .font(FrameMioTypography.headline)
                    .foregroundStyle(FrameMioColors.accent)
                    .accessibilityLabel("\(viewModel.selectedCount) selected")
            }
        }
        .safeAreaInset(edge: .bottom) {
            if !viewModel.isAccessBlocked && viewModel.totalAssetCount > 0 {
                createStoryAction
            }
        }
        .task { viewModel.load() }
        .alert("Photo access needed", isPresented: errorBinding) {
            Button("Try Again") { viewModel.load() }
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
    }

    private var mediaBrowser: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: FrameMioSpacing.xxl) {
                if viewModel.authorization == .limited {
                    limitedAccessNotice
                }

                filterPicker

                if viewModel.sections.isEmpty {
                    EmptyState(
                        systemImage: viewModel.filter == .videos ? "video.slash" : "photo.on.rectangle.angled",
                        title: "Nothing in this filter",
                        message: "Try another media filter to keep exploring your moments."
                    )
                    .frame(maxWidth: .infinity)
                } else {
                    ForEach(viewModel.sections) { section in
                        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                            Text(section.title)
                                .font(FrameMioTypography.eyebrow)
                                .foregroundStyle(FrameMioColors.secondaryInk)
                                .textCase(.uppercase)

                            LazyVGrid(columns: gridColumns, spacing: FrameMioSpacing.xxs) {
                                ForEach(section.assets) { asset in
                                    mediaCell(for: asset)
                                }
                            }
                        }
                    }
                }
            }
            .padding(.horizontal, FrameMioSpacing.md)
            .padding(.top, FrameMioSpacing.md)
            .padding(.bottom, FrameMioSpacing.xxl)
        }
        .scrollIndicators(.hidden)
    }

    private func mediaCell(for asset: MediaAsset) -> some View {
        MediaThumbnail(
            image: viewModel.image(for: asset),
            duration: asset.isVideo ? asset.duration : nil,
            isLivePhoto: asset.isLivePhoto,
            isSelected: viewModel.selectedIDs.contains(asset.id),
            aspectRatio: FrameMioMetrics.thumbnailAspectRatio,
            accessibilityTitle: accessibilityTitle(for: asset),
            action: { viewModel.toggleSelection(for: asset) }
        )
        .onAppear {
            viewModel.loadThumbnail(
                for: asset,
                targetSize: FrameMioMetrics.mediaThumbnailRequestSize
            )
        }
        .onDisappear {
            viewModel.cancelThumbnail(for: asset)
        }
    }

    private var filterPicker: some View {
        Picker(
            "Media type",
            selection: Binding(
                get: { viewModel.filter },
                set: { viewModel.setFilter($0) }
            )
        ) {
            ForEach(PhotoLibraryMediaFilter.allCases, id: \.self) { filter in
                Text(filter.title).tag(filter)
            }
        }
        .pickerStyle(.segmented)
        .accessibilityLabel("Filter media")
    }

    private var limitedAccessNotice: some View {
        HStack(alignment: .top, spacing: FrameMioSpacing.sm) {
            Image(systemName: "info.circle")
                .foregroundStyle(FrameMioColors.accent)

            Text("Showing the photos and videos you chose to share with FrameMio.")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)
        }
        .padding(FrameMioSpacing.md)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(FrameMioColors.accentMuted, in: RoundedRectangle(cornerRadius: FrameMioRadius.medium, style: .continuous))
        .accessibilityElement(children: .combine)
    }

    private var accessState: some View {
        EmptyState(
            systemImage: viewModel.authorization == .restricted ? "lock.fill" : "photo.on.rectangle.angled",
            title: viewModel.accessTitle,
            message: viewModel.accessMessage,
            actionTitle: "Open Settings",
            action: openSettings
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var createStoryAction: some View {
        VStack(spacing: FrameMioSpacing.xs) {
            Text(selectionSummary)
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.secondaryInk)

            PrimaryButton(title: "Create Story", systemImage: "sparkles") {
                onContinue(viewModel.selectedAssets)
            }
            .disabled(!viewModel.canContinue)
        }
        .padding(.horizontal, FrameMioSpacing.lg)
        .padding(.top, FrameMioSpacing.sm)
        .padding(.bottom, FrameMioSpacing.xs)
        .background(.regularMaterial)
    }

    private var selectionSummary: String {
        switch viewModel.selectedCount {
        case 0: "Select the moments that belong together"
        case 1: "1 moment selected"
        default: "\(viewModel.selectedCount) moments selected"
        }
    }

    private var gridColumns: [GridItem] {
        Array(
            repeating: GridItem(.flexible(), spacing: FrameMioSpacing.xxs),
            count: FrameMioMetrics.mediaGridColumnCount
        )
    }

    private func accessibilityTitle(for asset: MediaAsset) -> String {
        if asset.isVideo { return "Video" }
        if asset.isLivePhoto { return "Live Photo" }
        return "Photo"
    }

    private func openSettings() {
        guard let settingsURL = URL(string: UIApplication.openSettingsURLString) else { return }
        openURL(settingsURL)
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { viewModel.errorMessage != nil },
            set: { if !$0 { viewModel.errorMessage = nil } }
        )
    }
}
