import 'package:flutter/material.dart';

/// A quiet, elegant page transition: fade with a short upward settle.
/// Slow curve, no bounce — NP4's motion language is calm and deliberate.
class FadeSlideRoute<T> extends PageRouteBuilder<T> {
  FadeSlideRoute({required WidgetBuilder builder})
      : super(
          transitionDuration: const Duration(milliseconds: 420),
          reverseTransitionDuration: const Duration(milliseconds: 240),
          pageBuilder: (ctx, animation, secondary) => builder(ctx),
          transitionsBuilder: (ctx, animation, secondary, child) {
            final curved =
                CurvedAnimation(parent: animation, curve: Curves.easeOutCubic);
            return FadeTransition(
              opacity: curved,
              child: SlideTransition(
                position: Tween(begin: const Offset(0, 0.045), end: Offset.zero)
                    .animate(curved),
                child: child,
              ),
            );
          },
        );
}

/// Entrance wrapper for staggered choreography: [fx] runs 0→1 once; each
/// element maps its own [interval] onto it, so sections settle in sequence
/// rather than all at once.
class StaggerIn extends StatelessWidget {
  const StaggerIn({
    super.key,
    required this.fx,
    required this.interval,
    required this.child,
  });

  final Animation<double> fx;
  final Interval interval;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final anim = CurvedAnimation(parent: fx, curve: interval);
    return AnimatedBuilder(
      animation: anim,
      builder: (ctx, built) => Opacity(
        opacity: anim.value.clamp(0, 1),
        child: Transform.translate(
          offset: Offset(0, 22 * (1 - anim.value)),
          child: built,
        ),
      ),
      child: child,
    );
  }
}
