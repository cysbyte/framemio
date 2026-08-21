import SwiftUI

struct StoryCreationView: View {
    @ObservedObject private var viewModel: StoryCreationViewModel
    let onCreated: (Story) -> Void

    init(viewModel: StoryCreationViewModel, onCreated: @escaping (Story) -> Void) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
        self.onCreated = onCreated
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: FrameMioSpacing.section) {
                VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                    Text("A story from \(viewModel.context.rawValue.lowercased()).")
                        .font(FrameMioTypography.display)
                        .foregroundStyle(FrameMioColors.ink)
                    Text("FrameMio will look for the feeling that connects these moments, then shape them into something worth revisiting.")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }

                SectionHeader(title: "Selected moments", subtitle: "\(viewModel.assets.count) photos and videos")
                LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible()), GridItem(.flexible())], spacing: FrameMioSpacing.sm) {
                    ForEach(viewModel.assets) { asset in
                        MediaThumbnail(
                            duration: asset.isVideo ? asset.duration : nil,
                            isLivePhoto: asset.isLivePhoto,
                            accessibilityTitle: asset.isVideo ? "Selected video" : "Selected photo"
                        )
                    }
                }

                if viewModel.isCreating {
                    VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
                        LoadingState(title: viewModel.phaseTitle, message: "You can leave this screen; FrameMio will keep the work focused and quiet.")
                            .frame(maxWidth: .infinity)
                        FrameMioProgressView(value: viewModel.progress, title: "Creating your story")
                    }
                } else if viewModel.story == nil {
                    PrimaryButton(title: "Create Story", systemImage: "sparkles") {
                        viewModel.createStory()
                    }
                }

                if let errorMessage = viewModel.errorMessage {
                    Text(errorMessage)
                        .font(FrameMioTypography.footnote)
                        .foregroundStyle(FrameMioColors.accent)
                        .accessibilityAddTraits(.isStaticText)
                }
            }
            .padding(.horizontal, FrameMioSpacing.lg)
            .padding(.top, FrameMioSpacing.md)
            .padding(.bottom, FrameMioSpacing.xxxl)
        }
        .background(FrameMioColors.canvas)
        .navigationTitle("Create Story")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if viewModel.isCreating {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Cancel") { viewModel.cancel() }
                }
            }
        }
        .onChange(of: viewModel.story) { _, story in
            if let story { onCreated(story) }
        }
    }
}
