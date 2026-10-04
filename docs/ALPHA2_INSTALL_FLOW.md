# OnePane Alpha 2 installation flow

The operating-system installer installs OnePane itself and establishes the durable storage locations required by the harness.

## Interactive installation decisions

The installer retains **Model Pool Location** as an explicit choice because local model files may be very large and users may need to place them on a dedicated SSD or other high-capacity volume. Existing installations preserve their configured model pool.

The installer does **not** prompt for OmniRoute or Colibri. They are bundled OnePane managed components and are provisioned/controlled by the harness after OnePane has started and passed its own health check. Users manage their runtime state from OnePane with Enable/Disable controls.

## Required order

1. System preflight.
2. Establish OnePane application/data paths.
3. Choose or preserve Model Pool Location.
4. Install OnePane service and desktop application.
5. Start and health-check the OnePane harness.
6. Launch OnePane and first-run Tour.
7. Harness provisions bundled managed components independently of core harness health.

Colibri or OmniRoute component failure must never make the OnePane service unhealthy.
