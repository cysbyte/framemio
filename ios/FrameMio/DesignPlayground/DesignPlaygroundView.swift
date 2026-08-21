import SwiftUI

struct DesignPlaygroundView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var isSelected = true
    @State private var progress = 0.64

    var body: some View {
        NavigationStack {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: FrameMioSpacing.section) {
                    introduction
                    heroMemory
                    primaryActions
                    recentStory
                    momentSelection
                    creationState
                    quietStates
                    visualLanguage
                }
                .padding(.horizontal, FrameMioSpacing.lg)
                .padding(.top, FrameMioSpacing.lg)
                .padding(.bottom, FrameMioSpacing.section)
            }
            .background(FrameMioColors.canvas.ignoresSafeArea())
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Text("FrameMio")
                        .font(FrameMioTypography.headline)
                        .foregroundStyle(FrameMioColors.ink)
                }
            }
            .navigationBarTitleDisplayMode(.inline)
        }
    }

    private var introduction: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("FOR YOU")
                .font(FrameMioTypography.eyebrow)
                .tracking(FrameMioSpacing.xxs / 2)
                .foregroundStyle(FrameMioColors.accent)

            Text("Your life, beautifully remembered.")
                .font(FrameMioTypography.display)
                .foregroundStyle(FrameMioColors.ink)

            Text("A quiet place for the photos and videos that mean more with time.")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var heroMemory: some View {
        ZStack(alignment: .bottomLeading) {
            MemoryArtwork(systemImage: "mountain.2.fill")

            LinearGradient(
                colors: [.clear, FrameMioColors.mediaScrim],
                startPoint: .center,
                endPoint: .bottom
            )

            VStack(alignment: .leading, spacing: FrameMioSpacing.xs) {
                Text("AUGUST · WEEKEND")
                    .font(FrameMioTypography.eyebrow)
                    .tracking(FrameMioSpacing.xxs / 2)
                    .foregroundStyle(FrameMioColors.onMedia)

                Text("Two slow days by the water")
                    .font(FrameMioTypography.editorialTitle)
                    .foregroundStyle(FrameMioColors.onMedia)

                VideoDurationBadge(duration: 54)
            }
            .padding(FrameMioSpacing.xl)
        }
        .aspectRatio(FrameMioMetrics.thumbnailAspectRatio, contentMode: .fit)
        .clipShape(RoundedRectangle(cornerRadius: FrameMioRadius.extraLarge, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: FrameMioRadius.extraLarge, style: .continuous)
                .stroke(FrameMioColors.divider, lineWidth: FrameMioMetrics.borderWidth)
        }
        .frameMioElevation(.subtle)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Featured memory, Two slow days by the water, 54 seconds")
    }

    private var primaryActions: some View {
        VStack(spacing: FrameMioSpacing.sm) {
            PrimaryButton(title: "Create a memory", systemImage: "plus") {
                withAnimation(FrameMioAnimation.gentle(reduceMotion: reduceMotion)) {
                    progress = min(progress + 0.12, 1)
                }
            }

            SecondaryButton(title: "Choose moments", systemImage: "photo.on.rectangle") {
                FrameMioHaptics.secondaryAction()
            }
        }
    }

    private var recentStory: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(
                title: "Recently remembered",
                subtitle: "Stories shaped around the feeling of the moment",
                actionTitle: "See all"
            ) {}

            StoryCard(
                title: "Sunday at home",
                subtitle: "18 moments · Family",
                duration: 48
            )
        }
    }

    private var momentSelection: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.md) {
            SectionHeader(
                title: "Moments from today",
                subtitle: "A few frames that already belong together"
            )

            LazyVGrid(
                columns: [
                    GridItem(.flexible(), spacing: FrameMioSpacing.sm),
                    GridItem(.flexible(), spacing: FrameMioSpacing.sm)
                ],
                spacing: FrameMioSpacing.sm
            ) {
                MediaThumbnail(
                    duration: 12,
                    isSelected: isSelected,
                    placeholderSystemImage: "sun.horizon.fill",
                    accessibilityTitle: "Golden-hour walk"
                ) {
                    FrameMioHaptics.selection()
                    withAnimation(FrameMioAnimation.selection(reduceMotion: reduceMotion)) {
                        isSelected.toggle()
                    }
                }

                MediaThumbnail(
                    placeholderSystemImage: "figure.2.and.child.holdinghands",
                    accessibilityTitle: "Family moment"
                )

                MediaThumbnail(
                    duration: 8,
                    placeholderSystemImage: "pawprint.fill",
                    accessibilityTitle: "Pet video"
                )

                MediaThumbnail(
                    placeholderSystemImage: "cup.and.saucer.fill",
                    accessibilityTitle: "Morning coffee"
                )
            }
        }
    }

    private var creationState: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.lg) {
            SectionHeader(
                title: "Coming together",
                subtitle: "The experience stays quiet while the story takes shape"
            )

            FrameMioProgressView(value: progress, title: "Creating your memory")

            HStack(spacing: FrameMioSpacing.sm) {
                Tag("Weekend", systemImage: "calendar")
                Tag("Family", systemImage: "person.2")
            }

            SecondaryButton(title: "Preview completion", systemImage: "checkmark") {
                FrameMioHaptics.success()
                withAnimation(FrameMioAnimation.gentle(reduceMotion: reduceMotion)) {
                    progress = 1
                }
            }
        }
    }

    private var quietStates: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
            SectionHeader(
                title: "Quiet states",
                subtitle: "Helpful without becoming technical"
            )

            LoadingState(
                title: "Finding the rhythm",
                message: "Looking for the moments that feel natural together."
            )
            .frame(maxWidth: .infinity)

            Divider()
                .overlay(FrameMioColors.divider)

            EmptyState(
                systemImage: "photo.stack",
                title: "A new memory can begin here",
                message: "Choose a few photos or videos whenever the moment feels worth keeping.",
                actionTitle: "Choose moments"
            ) {}
            .frame(maxWidth: .infinity)
        }
    }

    private var visualLanguage: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.xl) {
            SectionHeader(
                title: "The visual language",
                subtitle: "Warm, editorial, and designed to disappear behind photography"
            )

            PalettePreview()
            TypographyPreview()
            SpacingPreview()
            ShapePreview()
        }
    }
}

private struct MemoryArtwork: View {
    let systemImage: String

    var body: some View {
        LinearGradient(
            colors: [FrameMioColors.memorySky, FrameMioColors.memoryLight, FrameMioColors.memoryEarth],
            startPoint: .topLeading,
            endPoint: .bottomTrailing
        )
        .overlay {
            Image(systemName: systemImage)
                .resizable()
                .scaledToFit()
                .symbolRenderingMode(.monochrome)
                .foregroundStyle(FrameMioColors.onMedia.opacity(FrameMioOpacity.artworkSymbol))
                .padding(FrameMioSpacing.xxxl)
                .accessibilityHidden(true)
        }
    }
}

private struct PalettePreview: View {
    private let swatches: [(String, Color)] = [
        ("Canvas", FrameMioColors.canvas),
        ("Surface", FrameMioColors.surface),
        ("Elevated", FrameMioColors.elevatedSurface),
        ("Ink", FrameMioColors.ink),
        ("Secondary", FrameMioColors.secondaryInk),
        ("Tertiary", FrameMioColors.tertiaryInk),
        ("Copper", FrameMioColors.accent),
        ("Muted copper", FrameMioColors.accentMuted),
        ("Selection", FrameMioColors.selection),
        ("Media", FrameMioColors.mediaPlaceholder)
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("A warm, neutral palette")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            ScrollView(.horizontal) {
                HStack(spacing: FrameMioSpacing.sm) {
                    ForEach(swatches, id: \.0) { name, color in
                        VStack(spacing: FrameMioSpacing.xs) {
                            RoundedRectangle(cornerRadius: FrameMioRadius.small, style: .continuous)
                                .fill(color)
                                .frame(
                                    width: FrameMioMetrics.minimumTouchTarget,
                                    height: FrameMioMetrics.minimumTouchTarget
                                )
                                .overlay {
                                    RoundedRectangle(cornerRadius: FrameMioRadius.small, style: .continuous)
                                        .stroke(FrameMioColors.divider, lineWidth: FrameMioMetrics.borderWidth)
                                }

                            Text(name)
                                .font(FrameMioTypography.eyebrow)
                                .foregroundStyle(FrameMioColors.secondaryInk)
                                .lineLimit(1)
                        }
                    }
                }
            }
            .scrollIndicators(.hidden)
        }
    }
}

private struct TypographyPreview: View {
    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("MEMORY 07")
                .font(FrameMioTypography.eyebrow)
                .tracking(FrameMioSpacing.xxs / 2)
                .foregroundStyle(FrameMioColors.accent)
            Text("August light")
                .font(FrameMioTypography.display)
                .foregroundStyle(FrameMioColors.ink)
            Text("A weekend by the water")
                .font(FrameMioTypography.editorialTitle)
                .foregroundStyle(FrameMioColors.ink)
            Text("Twelve moments, gently brought together")
                .font(FrameMioTypography.title)
                .foregroundStyle(FrameMioColors.ink)
            Text("The story found its own rhythm.")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)
            Text("Typography supports the emotion without competing with the image.")
                .font(FrameMioTypography.body)
                .foregroundStyle(FrameMioColors.secondaryInk)
            Text("Native controls remain familiar and easy to scan.")
                .font(FrameMioTypography.callout)
                .foregroundStyle(FrameMioColors.secondaryInk)
            Text("Personal, not performative")
                .font(FrameMioTypography.caption)
                .foregroundStyle(FrameMioColors.tertiaryInk)
            Text("Dynamic Type supported")
                .font(FrameMioTypography.footnote)
                .foregroundStyle(FrameMioColors.tertiaryInk)
        }
    }
}

private struct SpacingPreview: View {
    private let values = [
        FrameMioSpacing.xxs,
        FrameMioSpacing.xs,
        FrameMioSpacing.sm,
        FrameMioSpacing.md,
        FrameMioSpacing.lg,
        FrameMioSpacing.xl,
        FrameMioSpacing.xxl,
        FrameMioSpacing.xxxl,
        FrameMioSpacing.section
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("Breathing room")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            HStack(alignment: .bottom, spacing: FrameMioSpacing.xs) {
                ForEach(Array(values.enumerated()), id: \.offset) { _, value in
                    RoundedRectangle(cornerRadius: FrameMioRadius.small, style: .continuous)
                        .fill(FrameMioColors.accentMuted)
                        .frame(width: FrameMioSpacing.xs, height: value)
                        .accessibilityLabel("Spacing, \(Int(value)) points")
                }
            }
        }
    }
}

private struct ShapePreview: View {
    var body: some View {
        VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
            Text("Soft edges, restrained depth")
                .font(FrameMioTypography.headline)
                .foregroundStyle(FrameMioColors.ink)

            HStack(spacing: FrameMioSpacing.sm) {
                ShapeSample(radius: FrameMioRadius.small, elevation: .none)
                ShapeSample(radius: FrameMioRadius.medium, elevation: .subtle)
                ShapeSample(radius: FrameMioRadius.large, elevation: .floating)
                ShapeSample(radius: FrameMioRadius.extraLarge, elevation: .none)
            }
        }
    }
}

private struct ShapeSample: View {
    let radius: CGFloat
    let elevation: FrameMioElevation

    var body: some View {
        RoundedRectangle(cornerRadius: radius, style: .continuous)
            .fill(FrameMioColors.elevatedSurface)
            .frame(maxWidth: .infinity, minHeight: FrameMioMetrics.buttonHeight)
            .frameMioElevation(elevation)
            .accessibilityLabel("Corner radius \(Int(radius)) points")
    }
}

#Preview {
    DesignPlaygroundView()
}

#Preview("Dark Appearance") {
    DesignPlaygroundView()
        .preferredColorScheme(.dark)
}
