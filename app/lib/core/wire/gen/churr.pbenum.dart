// This is a generated file - do not edit.
//
// Generated from churr/v1/churr.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

class ServerSignInRegistration extends $pb.ProtobufEnum {
  static const ServerSignInRegistration
      SERVER_SIGN_IN_REGISTRATION_UNSPECIFIED = ServerSignInRegistration._(
          0, _omitEnumNames ? '' : 'SERVER_SIGN_IN_REGISTRATION_UNSPECIFIED');
  static const ServerSignInRegistration SERVER_SIGN_IN_REGISTRATION_OPEN =
      ServerSignInRegistration._(
          1, _omitEnumNames ? '' : 'SERVER_SIGN_IN_REGISTRATION_OPEN');
  static const ServerSignInRegistration SERVER_SIGN_IN_REGISTRATION_INVITE =
      ServerSignInRegistration._(
          2, _omitEnumNames ? '' : 'SERVER_SIGN_IN_REGISTRATION_INVITE');
  static const ServerSignInRegistration SERVER_SIGN_IN_REGISTRATION_CLOSED =
      ServerSignInRegistration._(
          3, _omitEnumNames ? '' : 'SERVER_SIGN_IN_REGISTRATION_CLOSED');

  static const $core.List<ServerSignInRegistration> values =
      <ServerSignInRegistration>[
    SERVER_SIGN_IN_REGISTRATION_UNSPECIFIED,
    SERVER_SIGN_IN_REGISTRATION_OPEN,
    SERVER_SIGN_IN_REGISTRATION_INVITE,
    SERVER_SIGN_IN_REGISTRATION_CLOSED,
  ];

  static final $core.List<ServerSignInRegistration?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ServerSignInRegistration? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ServerSignInRegistration._(super.value, super.name);
}

class ServerSignInMemberSignIn extends $pb.ProtobufEnum {
  static const ServerSignInMemberSignIn
      SERVER_SIGN_IN_MEMBER_SIGN_IN_UNSPECIFIED = ServerSignInMemberSignIn._(
          0, _omitEnumNames ? '' : 'SERVER_SIGN_IN_MEMBER_SIGN_IN_UNSPECIFIED');
  static const ServerSignInMemberSignIn SERVER_SIGN_IN_MEMBER_SIGN_IN_PASSKEY =
      ServerSignInMemberSignIn._(
          1, _omitEnumNames ? '' : 'SERVER_SIGN_IN_MEMBER_SIGN_IN_PASSKEY');
  static const ServerSignInMemberSignIn SERVER_SIGN_IN_MEMBER_SIGN_IN_EITHER =
      ServerSignInMemberSignIn._(
          2, _omitEnumNames ? '' : 'SERVER_SIGN_IN_MEMBER_SIGN_IN_EITHER');
  static const ServerSignInMemberSignIn SERVER_SIGN_IN_MEMBER_SIGN_IN_SSO =
      ServerSignInMemberSignIn._(
          3, _omitEnumNames ? '' : 'SERVER_SIGN_IN_MEMBER_SIGN_IN_SSO');

  static const $core.List<ServerSignInMemberSignIn> values =
      <ServerSignInMemberSignIn>[
    SERVER_SIGN_IN_MEMBER_SIGN_IN_UNSPECIFIED,
    SERVER_SIGN_IN_MEMBER_SIGN_IN_PASSKEY,
    SERVER_SIGN_IN_MEMBER_SIGN_IN_EITHER,
    SERVER_SIGN_IN_MEMBER_SIGN_IN_SSO,
  ];

  static final $core.List<ServerSignInMemberSignIn?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ServerSignInMemberSignIn? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ServerSignInMemberSignIn._(super.value, super.name);
}

class ServerSignInSecondFactor extends $pb.ProtobufEnum {
  static const ServerSignInSecondFactor
      SERVER_SIGN_IN_SECOND_FACTOR_UNSPECIFIED = ServerSignInSecondFactor._(
          0, _omitEnumNames ? '' : 'SERVER_SIGN_IN_SECOND_FACTOR_UNSPECIFIED');
  static const ServerSignInSecondFactor SERVER_SIGN_IN_SECOND_FACTOR_OFF =
      ServerSignInSecondFactor._(
          1, _omitEnumNames ? '' : 'SERVER_SIGN_IN_SECOND_FACTOR_OFF');
  static const ServerSignInSecondFactor SERVER_SIGN_IN_SECOND_FACTOR_REQUIRED =
      ServerSignInSecondFactor._(
          2, _omitEnumNames ? '' : 'SERVER_SIGN_IN_SECOND_FACTOR_REQUIRED');

  static const $core.List<ServerSignInSecondFactor> values =
      <ServerSignInSecondFactor>[
    SERVER_SIGN_IN_SECOND_FACTOR_UNSPECIFIED,
    SERVER_SIGN_IN_SECOND_FACTOR_OFF,
    SERVER_SIGN_IN_SECOND_FACTOR_REQUIRED,
  ];

  static final $core.List<ServerSignInSecondFactor?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ServerSignInSecondFactor? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ServerSignInSecondFactor._(super.value, super.name);
}

class SsoProviderSummaryKind extends $pb.ProtobufEnum {
  static const SsoProviderSummaryKind SSO_PROVIDER_SUMMARY_KIND_UNSPECIFIED =
      SsoProviderSummaryKind._(
          0, _omitEnumNames ? '' : 'SSO_PROVIDER_SUMMARY_KIND_UNSPECIFIED');
  static const SsoProviderSummaryKind SSO_PROVIDER_SUMMARY_KIND_OIDC =
      SsoProviderSummaryKind._(
          1, _omitEnumNames ? '' : 'SSO_PROVIDER_SUMMARY_KIND_OIDC');
  static const SsoProviderSummaryKind SSO_PROVIDER_SUMMARY_KIND_SAML =
      SsoProviderSummaryKind._(
          2, _omitEnumNames ? '' : 'SSO_PROVIDER_SUMMARY_KIND_SAML');

  static const $core.List<SsoProviderSummaryKind> values =
      <SsoProviderSummaryKind>[
    SSO_PROVIDER_SUMMARY_KIND_UNSPECIFIED,
    SSO_PROVIDER_SUMMARY_KIND_OIDC,
    SSO_PROVIDER_SUMMARY_KIND_SAML,
  ];

  static final $core.List<SsoProviderSummaryKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static SsoProviderSummaryKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SsoProviderSummaryKind._(super.value, super.name);
}

class ServerPushKinds extends $pb.ProtobufEnum {
  static const ServerPushKinds SERVER_PUSH_KINDS_UNSPECIFIED =
      ServerPushKinds._(
          0, _omitEnumNames ? '' : 'SERVER_PUSH_KINDS_UNSPECIFIED');
  static const ServerPushKinds SERVER_PUSH_KINDS_APNS =
      ServerPushKinds._(1, _omitEnumNames ? '' : 'SERVER_PUSH_KINDS_APNS');
  static const ServerPushKinds SERVER_PUSH_KINDS_FCM =
      ServerPushKinds._(2, _omitEnumNames ? '' : 'SERVER_PUSH_KINDS_FCM');
  static const ServerPushKinds SERVER_PUSH_KINDS_UNIFIEDPUSH =
      ServerPushKinds._(
          3, _omitEnumNames ? '' : 'SERVER_PUSH_KINDS_UNIFIEDPUSH');
  static const ServerPushKinds SERVER_PUSH_KINDS_NONE =
      ServerPushKinds._(4, _omitEnumNames ? '' : 'SERVER_PUSH_KINDS_NONE');

  static const $core.List<ServerPushKinds> values = <ServerPushKinds>[
    SERVER_PUSH_KINDS_UNSPECIFIED,
    SERVER_PUSH_KINDS_APNS,
    SERVER_PUSH_KINDS_FCM,
    SERVER_PUSH_KINDS_UNIFIEDPUSH,
    SERVER_PUSH_KINDS_NONE,
  ];

  static final $core.List<ServerPushKinds?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static ServerPushKinds? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ServerPushKinds._(super.value, super.name);
}

class CardThemeKind extends $pb.ProtobufEnum {
  static const CardThemeKind CARD_THEME_KIND_UNSPECIFIED =
      CardThemeKind._(0, _omitEnumNames ? '' : 'CARD_THEME_KIND_UNSPECIFIED');
  static const CardThemeKind CARD_THEME_KIND_DARK =
      CardThemeKind._(1, _omitEnumNames ? '' : 'CARD_THEME_KIND_DARK');
  static const CardThemeKind CARD_THEME_KIND_LIGHT =
      CardThemeKind._(2, _omitEnumNames ? '' : 'CARD_THEME_KIND_LIGHT');

  static const $core.List<CardThemeKind> values = <CardThemeKind>[
    CARD_THEME_KIND_UNSPECIFIED,
    CARD_THEME_KIND_DARK,
    CARD_THEME_KIND_LIGHT,
  ];

  static final $core.List<CardThemeKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static CardThemeKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CardThemeKind._(super.value, super.name);
}

class ErrorErrorCode extends $pb.ProtobufEnum {
  static const ErrorErrorCode ERROR_ERROR_CODE_UNSPECIFIED =
      ErrorErrorCode._(0, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_UNSPECIFIED');
  static const ErrorErrorCode ERROR_ERROR_CODE_INVALID_REQUEST =
      ErrorErrorCode._(
          1, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_INVALID_REQUEST');
  static const ErrorErrorCode ERROR_ERROR_CODE_INVALID_VALUE = ErrorErrorCode._(
      2, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_INVALID_VALUE');
  static const ErrorErrorCode ERROR_ERROR_CODE_UNAUTHENTICATED =
      ErrorErrorCode._(
          3, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_UNAUTHENTICATED');
  static const ErrorErrorCode ERROR_ERROR_CODE_SECOND_FACTOR_REQUIRED =
      ErrorErrorCode._(
          4, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_SECOND_FACTOR_REQUIRED');
  static const ErrorErrorCode ERROR_ERROR_CODE_FORBIDDEN =
      ErrorErrorCode._(5, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_FORBIDDEN');
  static const ErrorErrorCode ERROR_ERROR_CODE_TIMED_OUT =
      ErrorErrorCode._(6, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_TIMED_OUT');
  static const ErrorErrorCode ERROR_ERROR_CODE_ARCHIVED =
      ErrorErrorCode._(7, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_ARCHIVED');
  static const ErrorErrorCode ERROR_ERROR_CODE_LOCKED =
      ErrorErrorCode._(8, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_LOCKED');
  static const ErrorErrorCode ERROR_ERROR_CODE_FEATURE_OFF =
      ErrorErrorCode._(9, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_FEATURE_OFF');
  static const ErrorErrorCode ERROR_ERROR_CODE_FRESH_CHECK_REQUIRED =
      ErrorErrorCode._(
          10, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_FRESH_CHECK_REQUIRED');
  static const ErrorErrorCode ERROR_ERROR_CODE_NOT_FOUND =
      ErrorErrorCode._(11, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_NOT_FOUND');
  static const ErrorErrorCode ERROR_ERROR_CODE_CONFLICT =
      ErrorErrorCode._(12, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_CONFLICT');
  static const ErrorErrorCode ERROR_ERROR_CODE_NAME_TAKEN =
      ErrorErrorCode._(13, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_NAME_TAKEN');
  static const ErrorErrorCode ERROR_ERROR_CODE_ALREADY_EXISTS =
      ErrorErrorCode._(
          14, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_ALREADY_EXISTS');
  static const ErrorErrorCode ERROR_ERROR_CODE_TOO_LARGE =
      ErrorErrorCode._(15, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_TOO_LARGE');
  static const ErrorErrorCode ERROR_ERROR_CODE_BLOCKED_BY_FILTER =
      ErrorErrorCode._(
          16, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_BLOCKED_BY_FILTER');
  static const ErrorErrorCode ERROR_ERROR_CODE_RATE_LIMITED = ErrorErrorCode._(
      17, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_RATE_LIMITED');
  static const ErrorErrorCode ERROR_ERROR_CODE_SLOW_MODE =
      ErrorErrorCode._(18, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_SLOW_MODE');
  static const ErrorErrorCode ERROR_ERROR_CODE_INTERNAL =
      ErrorErrorCode._(19, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_INTERNAL');
  static const ErrorErrorCode ERROR_ERROR_CODE_UNAVAILABLE = ErrorErrorCode._(
      20, _omitEnumNames ? '' : 'ERROR_ERROR_CODE_UNAVAILABLE');

  static const $core.List<ErrorErrorCode> values = <ErrorErrorCode>[
    ERROR_ERROR_CODE_UNSPECIFIED,
    ERROR_ERROR_CODE_INVALID_REQUEST,
    ERROR_ERROR_CODE_INVALID_VALUE,
    ERROR_ERROR_CODE_UNAUTHENTICATED,
    ERROR_ERROR_CODE_SECOND_FACTOR_REQUIRED,
    ERROR_ERROR_CODE_FORBIDDEN,
    ERROR_ERROR_CODE_TIMED_OUT,
    ERROR_ERROR_CODE_ARCHIVED,
    ERROR_ERROR_CODE_LOCKED,
    ERROR_ERROR_CODE_FEATURE_OFF,
    ERROR_ERROR_CODE_FRESH_CHECK_REQUIRED,
    ERROR_ERROR_CODE_NOT_FOUND,
    ERROR_ERROR_CODE_CONFLICT,
    ERROR_ERROR_CODE_NAME_TAKEN,
    ERROR_ERROR_CODE_ALREADY_EXISTS,
    ERROR_ERROR_CODE_TOO_LARGE,
    ERROR_ERROR_CODE_BLOCKED_BY_FILTER,
    ERROR_ERROR_CODE_RATE_LIMITED,
    ERROR_ERROR_CODE_SLOW_MODE,
    ERROR_ERROR_CODE_INTERNAL,
    ERROR_ERROR_CODE_UNAVAILABLE,
  ];

  static final $core.List<ErrorErrorCode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 20);
  static ErrorErrorCode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ErrorErrorCode._(super.value, super.name);
}

class WellKnownKind extends $pb.ProtobufEnum {
  static const WellKnownKind WELL_KNOWN_KIND_UNSPECIFIED =
      WellKnownKind._(0, _omitEnumNames ? '' : 'WELL_KNOWN_KIND_UNSPECIFIED');
  static const WellKnownKind WELL_KNOWN_KIND_SERVER =
      WellKnownKind._(1, _omitEnumNames ? '' : 'WELL_KNOWN_KIND_SERVER');
  static const WellKnownKind WELL_KNOWN_KIND_HUB =
      WellKnownKind._(2, _omitEnumNames ? '' : 'WELL_KNOWN_KIND_HUB');

  static const $core.List<WellKnownKind> values = <WellKnownKind>[
    WELL_KNOWN_KIND_UNSPECIFIED,
    WELL_KNOWN_KIND_SERVER,
    WELL_KNOWN_KIND_HUB,
  ];

  static final $core.List<WellKnownKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static WellKnownKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const WellKnownKind._(super.value, super.name);
}

class SessionKind extends $pb.ProtobufEnum {
  static const SessionKind SESSION_KIND_UNSPECIFIED =
      SessionKind._(0, _omitEnumNames ? '' : 'SESSION_KIND_UNSPECIFIED');
  static const SessionKind SESSION_KIND_WEB =
      SessionKind._(1, _omitEnumNames ? '' : 'SESSION_KIND_WEB');
  static const SessionKind SESSION_KIND_APP =
      SessionKind._(2, _omitEnumNames ? '' : 'SESSION_KIND_APP');
  static const SessionKind SESSION_KIND_PERSONAL =
      SessionKind._(3, _omitEnumNames ? '' : 'SESSION_KIND_PERSONAL');
  static const SessionKind SESSION_KIND_BOT =
      SessionKind._(4, _omitEnumNames ? '' : 'SESSION_KIND_BOT');

  static const $core.List<SessionKind> values = <SessionKind>[
    SESSION_KIND_UNSPECIFIED,
    SESSION_KIND_WEB,
    SESSION_KIND_APP,
    SESSION_KIND_PERSONAL,
    SESSION_KIND_BOT,
  ];

  static final $core.List<SessionKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static SessionKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SessionKind._(super.value, super.name);
}

class SessionSignedInBy extends $pb.ProtobufEnum {
  static const SessionSignedInBy SESSION_SIGNED_IN_BY_UNSPECIFIED =
      SessionSignedInBy._(
          0, _omitEnumNames ? '' : 'SESSION_SIGNED_IN_BY_UNSPECIFIED');
  static const SessionSignedInBy SESSION_SIGNED_IN_BY_PASSKEY =
      SessionSignedInBy._(
          1, _omitEnumNames ? '' : 'SESSION_SIGNED_IN_BY_PASSKEY');
  static const SessionSignedInBy SESSION_SIGNED_IN_BY_ACCESS_KEY =
      SessionSignedInBy._(
          7, _omitEnumNames ? '' : 'SESSION_SIGNED_IN_BY_ACCESS_KEY');
  static const SessionSignedInBy SESSION_SIGNED_IN_BY_OIDC =
      SessionSignedInBy._(2, _omitEnumNames ? '' : 'SESSION_SIGNED_IN_BY_OIDC');
  static const SessionSignedInBy SESSION_SIGNED_IN_BY_SAML =
      SessionSignedInBy._(3, _omitEnumNames ? '' : 'SESSION_SIGNED_IN_BY_SAML');
  static const SessionSignedInBy SESSION_SIGNED_IN_BY_RECOVERY_CODE =
      SessionSignedInBy._(
          4, _omitEnumNames ? '' : 'SESSION_SIGNED_IN_BY_RECOVERY_CODE');
  static const SessionSignedInBy SESSION_SIGNED_IN_BY_RECOVERY_LINK =
      SessionSignedInBy._(
          5, _omitEnumNames ? '' : 'SESSION_SIGNED_IN_BY_RECOVERY_LINK');
  static const SessionSignedInBy SESSION_SIGNED_IN_BY_PAIRING =
      SessionSignedInBy._(
          6, _omitEnumNames ? '' : 'SESSION_SIGNED_IN_BY_PAIRING');

  static const $core.List<SessionSignedInBy> values = <SessionSignedInBy>[
    SESSION_SIGNED_IN_BY_UNSPECIFIED,
    SESSION_SIGNED_IN_BY_PASSKEY,
    SESSION_SIGNED_IN_BY_ACCESS_KEY,
    SESSION_SIGNED_IN_BY_OIDC,
    SESSION_SIGNED_IN_BY_SAML,
    SESSION_SIGNED_IN_BY_RECOVERY_CODE,
    SESSION_SIGNED_IN_BY_RECOVERY_LINK,
    SESSION_SIGNED_IN_BY_PAIRING,
  ];

  static final $core.List<SessionSignedInBy?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static SessionSignedInBy? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SessionSignedInBy._(super.value, super.name);
}

class SessionSecondFactor extends $pb.ProtobufEnum {
  static const SessionSecondFactor SESSION_SECOND_FACTOR_UNSPECIFIED =
      SessionSecondFactor._(
          0, _omitEnumNames ? '' : 'SESSION_SECOND_FACTOR_UNSPECIFIED');
  static const SessionSecondFactor SESSION_SECOND_FACTOR_PASSKEY =
      SessionSecondFactor._(
          1, _omitEnumNames ? '' : 'SESSION_SECOND_FACTOR_PASSKEY');
  static const SessionSecondFactor SESSION_SECOND_FACTOR_TOTP =
      SessionSecondFactor._(
          2, _omitEnumNames ? '' : 'SESSION_SECOND_FACTOR_TOTP');
  static const SessionSecondFactor SESSION_SECOND_FACTOR_PROVIDER =
      SessionSecondFactor._(
          3, _omitEnumNames ? '' : 'SESSION_SECOND_FACTOR_PROVIDER');

  static const $core.List<SessionSecondFactor> values = <SessionSecondFactor>[
    SESSION_SECOND_FACTOR_UNSPECIFIED,
    SESSION_SECOND_FACTOR_PASSKEY,
    SESSION_SECOND_FACTOR_TOTP,
    SESSION_SECOND_FACTOR_PROVIDER,
  ];

  static final $core.List<SessionSecondFactor?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static SessionSecondFactor? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SessionSecondFactor._(super.value, super.name);
}

class MeRole extends $pb.ProtobufEnum {
  static const MeRole ME_ROLE_UNSPECIFIED =
      MeRole._(0, _omitEnumNames ? '' : 'ME_ROLE_UNSPECIFIED');
  static const MeRole ME_ROLE_OWNER =
      MeRole._(1, _omitEnumNames ? '' : 'ME_ROLE_OWNER');
  static const MeRole ME_ROLE_ADMIN =
      MeRole._(2, _omitEnumNames ? '' : 'ME_ROLE_ADMIN');
  static const MeRole ME_ROLE_MEMBER =
      MeRole._(3, _omitEnumNames ? '' : 'ME_ROLE_MEMBER');
  static const MeRole ME_ROLE_GUEST =
      MeRole._(4, _omitEnumNames ? '' : 'ME_ROLE_GUEST');
  static const MeRole ME_ROLE_BOT =
      MeRole._(5, _omitEnumNames ? '' : 'ME_ROLE_BOT');
  static const MeRole ME_ROLE_REMOTE =
      MeRole._(6, _omitEnumNames ? '' : 'ME_ROLE_REMOTE');

  static const $core.List<MeRole> values = <MeRole>[
    ME_ROLE_UNSPECIFIED,
    ME_ROLE_OWNER,
    ME_ROLE_ADMIN,
    ME_ROLE_MEMBER,
    ME_ROLE_GUEST,
    ME_ROLE_BOT,
    ME_ROLE_REMOTE,
  ];

  static final $core.List<MeRole?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static MeRole? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MeRole._(super.value, super.name);
}

class MePresence extends $pb.ProtobufEnum {
  static const MePresence ME_PRESENCE_UNSPECIFIED =
      MePresence._(0, _omitEnumNames ? '' : 'ME_PRESENCE_UNSPECIFIED');
  static const MePresence ME_PRESENCE_ONLINE =
      MePresence._(1, _omitEnumNames ? '' : 'ME_PRESENCE_ONLINE');
  static const MePresence ME_PRESENCE_AWAY =
      MePresence._(2, _omitEnumNames ? '' : 'ME_PRESENCE_AWAY');
  static const MePresence ME_PRESENCE_OFFLINE =
      MePresence._(3, _omitEnumNames ? '' : 'ME_PRESENCE_OFFLINE');

  static const $core.List<MePresence> values = <MePresence>[
    ME_PRESENCE_UNSPECIFIED,
    ME_PRESENCE_ONLINE,
    ME_PRESENCE_AWAY,
    ME_PRESENCE_OFFLINE,
  ];

  static final $core.List<MePresence?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static MePresence? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MePresence._(super.value, super.name);
}

class MeLastSeen extends $pb.ProtobufEnum {
  static const MeLastSeen ME_LAST_SEEN_UNSPECIFIED =
      MeLastSeen._(0, _omitEnumNames ? '' : 'ME_LAST_SEEN_UNSPECIFIED');
  static const MeLastSeen ME_LAST_SEEN_ONLINE =
      MeLastSeen._(1, _omitEnumNames ? '' : 'ME_LAST_SEEN_ONLINE');
  static const MeLastSeen ME_LAST_SEEN_RECENTLY =
      MeLastSeen._(2, _omitEnumNames ? '' : 'ME_LAST_SEEN_RECENTLY');
  static const MeLastSeen ME_LAST_SEEN_WITHIN_WEEK =
      MeLastSeen._(3, _omitEnumNames ? '' : 'ME_LAST_SEEN_WITHIN_WEEK');
  static const MeLastSeen ME_LAST_SEEN_LONG_AGO =
      MeLastSeen._(4, _omitEnumNames ? '' : 'ME_LAST_SEEN_LONG_AGO');

  static const $core.List<MeLastSeen> values = <MeLastSeen>[
    ME_LAST_SEEN_UNSPECIFIED,
    ME_LAST_SEEN_ONLINE,
    ME_LAST_SEEN_RECENTLY,
    ME_LAST_SEEN_WITHIN_WEEK,
    ME_LAST_SEEN_LONG_AGO,
  ];

  static final $core.List<MeLastSeen?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static MeLastSeen? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MeLastSeen._(super.value, super.name);
}

class AvailabilityValue extends $pb.ProtobufEnum {
  static const AvailabilityValue AVAILABILITY_VALUE_UNSPECIFIED =
      AvailabilityValue._(
          0, _omitEnumNames ? '' : 'AVAILABILITY_VALUE_UNSPECIFIED');
  static const AvailabilityValue AVAILABILITY_VALUE_MEETING =
      AvailabilityValue._(
          1, _omitEnumNames ? '' : 'AVAILABILITY_VALUE_MEETING');
  static const AvailabilityValue AVAILABILITY_VALUE_OUT_OF_OFFICE =
      AvailabilityValue._(
          2, _omitEnumNames ? '' : 'AVAILABILITY_VALUE_OUT_OF_OFFICE');

  static const $core.List<AvailabilityValue> values = <AvailabilityValue>[
    AVAILABILITY_VALUE_UNSPECIFIED,
    AVAILABILITY_VALUE_MEETING,
    AVAILABILITY_VALUE_OUT_OF_OFFICE,
  ];

  static final $core.List<AvailabilityValue?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static AvailabilityValue? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AvailabilityValue._(super.value, super.name);
}

class AvailabilitySetBy extends $pb.ProtobufEnum {
  static const AvailabilitySetBy AVAILABILITY_SET_BY_UNSPECIFIED =
      AvailabilitySetBy._(
          0, _omitEnumNames ? '' : 'AVAILABILITY_SET_BY_UNSPECIFIED');
  static const AvailabilitySetBy AVAILABILITY_SET_BY_SELF =
      AvailabilitySetBy._(1, _omitEnumNames ? '' : 'AVAILABILITY_SET_BY_SELF');
  static const AvailabilitySetBy AVAILABILITY_SET_BY_INTEGRATION =
      AvailabilitySetBy._(
          2, _omitEnumNames ? '' : 'AVAILABILITY_SET_BY_INTEGRATION');

  static const $core.List<AvailabilitySetBy> values = <AvailabilitySetBy>[
    AVAILABILITY_SET_BY_UNSPECIFIED,
    AVAILABILITY_SET_BY_SELF,
    AVAILABILITY_SET_BY_INTEGRATION,
  ];

  static final $core.List<AvailabilitySetBy?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static AvailabilitySetBy? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AvailabilitySetBy._(super.value, super.name);
}

class PrefsThreadView extends $pb.ProtobufEnum {
  static const PrefsThreadView PREFS_THREAD_VIEW_UNSPECIFIED =
      PrefsThreadView._(
          0, _omitEnumNames ? '' : 'PREFS_THREAD_VIEW_UNSPECIFIED');
  static const PrefsThreadView PREFS_THREAD_VIEW_THREADED =
      PrefsThreadView._(1, _omitEnumNames ? '' : 'PREFS_THREAD_VIEW_THREADED');
  static const PrefsThreadView PREFS_THREAD_VIEW_FLAT =
      PrefsThreadView._(2, _omitEnumNames ? '' : 'PREFS_THREAD_VIEW_FLAT');

  static const $core.List<PrefsThreadView> values = <PrefsThreadView>[
    PREFS_THREAD_VIEW_UNSPECIFIED,
    PREFS_THREAD_VIEW_THREADED,
    PREFS_THREAD_VIEW_FLAT,
  ];

  static final $core.List<PrefsThreadView?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static PrefsThreadView? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PrefsThreadView._(super.value, super.name);
}

class DndSources extends $pb.ProtobufEnum {
  static const DndSources DND_SOURCES_UNSPECIFIED =
      DndSources._(0, _omitEnumNames ? '' : 'DND_SOURCES_UNSPECIFIED');
  static const DndSources DND_SOURCES_MANUAL =
      DndSources._(1, _omitEnumNames ? '' : 'DND_SOURCES_MANUAL');
  static const DndSources DND_SOURCES_STATUS =
      DndSources._(2, _omitEnumNames ? '' : 'DND_SOURCES_STATUS');
  static const DndSources DND_SOURCES_MEETING =
      DndSources._(3, _omitEnumNames ? '' : 'DND_SOURCES_MEETING');
  static const DndSources DND_SOURCES_QUIET =
      DndSources._(4, _omitEnumNames ? '' : 'DND_SOURCES_QUIET');

  static const $core.List<DndSources> values = <DndSources>[
    DND_SOURCES_UNSPECIFIED,
    DND_SOURCES_MANUAL,
    DND_SOURCES_STATUS,
    DND_SOURCES_MEETING,
    DND_SOURCES_QUIET,
  ];

  static final $core.List<DndSources?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static DndSources? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DndSources._(super.value, super.name);
}

class SecondFactorStateAccepted extends $pb.ProtobufEnum {
  static const SecondFactorStateAccepted
      SECOND_FACTOR_STATE_ACCEPTED_UNSPECIFIED = SecondFactorStateAccepted._(
          0, _omitEnumNames ? '' : 'SECOND_FACTOR_STATE_ACCEPTED_UNSPECIFIED');
  static const SecondFactorStateAccepted SECOND_FACTOR_STATE_ACCEPTED_PASSKEY =
      SecondFactorStateAccepted._(
          1, _omitEnumNames ? '' : 'SECOND_FACTOR_STATE_ACCEPTED_PASSKEY');
  static const SecondFactorStateAccepted SECOND_FACTOR_STATE_ACCEPTED_TOTP =
      SecondFactorStateAccepted._(
          2, _omitEnumNames ? '' : 'SECOND_FACTOR_STATE_ACCEPTED_TOTP');

  static const $core.List<SecondFactorStateAccepted> values =
      <SecondFactorStateAccepted>[
    SECOND_FACTOR_STATE_ACCEPTED_UNSPECIFIED,
    SECOND_FACTOR_STATE_ACCEPTED_PASSKEY,
    SECOND_FACTOR_STATE_ACCEPTED_TOTP,
  ];

  static final $core.List<SecondFactorStateAccepted?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static SecondFactorStateAccepted? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SecondFactorStateAccepted._(super.value, super.name);
}

class SecondFactorStateSetUp extends $pb.ProtobufEnum {
  static const SecondFactorStateSetUp SECOND_FACTOR_STATE_SET_UP_UNSPECIFIED =
      SecondFactorStateSetUp._(
          0, _omitEnumNames ? '' : 'SECOND_FACTOR_STATE_SET_UP_UNSPECIFIED');
  static const SecondFactorStateSetUp SECOND_FACTOR_STATE_SET_UP_PASSKEY =
      SecondFactorStateSetUp._(
          1, _omitEnumNames ? '' : 'SECOND_FACTOR_STATE_SET_UP_PASSKEY');
  static const SecondFactorStateSetUp SECOND_FACTOR_STATE_SET_UP_TOTP =
      SecondFactorStateSetUp._(
          2, _omitEnumNames ? '' : 'SECOND_FACTOR_STATE_SET_UP_TOTP');

  static const $core.List<SecondFactorStateSetUp> values =
      <SecondFactorStateSetUp>[
    SECOND_FACTOR_STATE_SET_UP_UNSPECIFIED,
    SECOND_FACTOR_STATE_SET_UP_PASSKEY,
    SECOND_FACTOR_STATE_SET_UP_TOTP,
  ];

  static final $core.List<SecondFactorStateSetUp?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static SecondFactorStateSetUp? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SecondFactorStateSetUp._(super.value, super.name);
}

class CheckInviteResponseRole extends $pb.ProtobufEnum {
  static const CheckInviteResponseRole CHECK_INVITE_RESPONSE_ROLE_UNSPECIFIED =
      CheckInviteResponseRole._(
          0, _omitEnumNames ? '' : 'CHECK_INVITE_RESPONSE_ROLE_UNSPECIFIED');
  static const CheckInviteResponseRole CHECK_INVITE_RESPONSE_ROLE_MEMBER =
      CheckInviteResponseRole._(
          1, _omitEnumNames ? '' : 'CHECK_INVITE_RESPONSE_ROLE_MEMBER');
  static const CheckInviteResponseRole CHECK_INVITE_RESPONSE_ROLE_GUEST =
      CheckInviteResponseRole._(
          2, _omitEnumNames ? '' : 'CHECK_INVITE_RESPONSE_ROLE_GUEST');

  static const $core.List<CheckInviteResponseRole> values =
      <CheckInviteResponseRole>[
    CHECK_INVITE_RESPONSE_ROLE_UNSPECIFIED,
    CHECK_INVITE_RESPONSE_ROLE_MEMBER,
    CHECK_INVITE_RESPONSE_ROLE_GUEST,
  ];

  static final $core.List<CheckInviteResponseRole?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static CheckInviteResponseRole? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CheckInviteResponseRole._(super.value, super.name);
}

class OAuthTokensTokenType extends $pb.ProtobufEnum {
  static const OAuthTokensTokenType OAUTH_TOKENS_TOKEN_TYPE_UNSPECIFIED =
      OAuthTokensTokenType._(
          0, _omitEnumNames ? '' : 'OAUTH_TOKENS_TOKEN_TYPE_UNSPECIFIED');
  static const OAuthTokensTokenType OAUTH_TOKENS_TOKEN_TYPE_BEARER =
      OAuthTokensTokenType._(
          1, _omitEnumNames ? '' : 'OAUTH_TOKENS_TOKEN_TYPE_BEARER');

  static const $core.List<OAuthTokensTokenType> values = <OAuthTokensTokenType>[
    OAUTH_TOKENS_TOKEN_TYPE_UNSPECIFIED,
    OAUTH_TOKENS_TOKEN_TYPE_BEARER,
  ];

  static final $core.List<OAuthTokensTokenType?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 1);
  static OAuthTokensTokenType? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const OAuthTokensTokenType._(super.value, super.name);
}

class OAuthErrorError extends $pb.ProtobufEnum {
  static const OAuthErrorError OAUTH_ERROR_ERROR_UNSPECIFIED =
      OAuthErrorError._(
          0, _omitEnumNames ? '' : 'OAUTH_ERROR_ERROR_UNSPECIFIED');
  static const OAuthErrorError OAUTH_ERROR_ERROR_INVALID_REQUEST =
      OAuthErrorError._(
          1, _omitEnumNames ? '' : 'OAUTH_ERROR_ERROR_INVALID_REQUEST');
  static const OAuthErrorError OAUTH_ERROR_ERROR_INVALID_GRANT =
      OAuthErrorError._(
          2, _omitEnumNames ? '' : 'OAUTH_ERROR_ERROR_INVALID_GRANT');
  static const OAuthErrorError OAUTH_ERROR_ERROR_UNSUPPORTED_GRANT_TYPE =
      OAuthErrorError._(
          3, _omitEnumNames ? '' : 'OAUTH_ERROR_ERROR_UNSUPPORTED_GRANT_TYPE');
  static const OAuthErrorError OAUTH_ERROR_ERROR_AUTHORIZATION_PENDING =
      OAuthErrorError._(
          4, _omitEnumNames ? '' : 'OAUTH_ERROR_ERROR_AUTHORIZATION_PENDING');
  static const OAuthErrorError OAUTH_ERROR_ERROR_SLOW_DOWN =
      OAuthErrorError._(5, _omitEnumNames ? '' : 'OAUTH_ERROR_ERROR_SLOW_DOWN');

  static const $core.List<OAuthErrorError> values = <OAuthErrorError>[
    OAUTH_ERROR_ERROR_UNSPECIFIED,
    OAUTH_ERROR_ERROR_INVALID_REQUEST,
    OAUTH_ERROR_ERROR_INVALID_GRANT,
    OAUTH_ERROR_ERROR_UNSUPPORTED_GRANT_TYPE,
    OAUTH_ERROR_ERROR_AUTHORIZATION_PENDING,
    OAUTH_ERROR_ERROR_SLOW_DOWN,
  ];

  static final $core.List<OAuthErrorError?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static OAuthErrorError? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const OAuthErrorError._(super.value, super.name);
}

class GetPairingResponseState extends $pb.ProtobufEnum {
  static const GetPairingResponseState GET_PAIRING_RESPONSE_STATE_UNSPECIFIED =
      GetPairingResponseState._(
          0, _omitEnumNames ? '' : 'GET_PAIRING_RESPONSE_STATE_UNSPECIFIED');
  static const GetPairingResponseState GET_PAIRING_RESPONSE_STATE_WAITING =
      GetPairingResponseState._(
          1, _omitEnumNames ? '' : 'GET_PAIRING_RESPONSE_STATE_WAITING');
  static const GetPairingResponseState GET_PAIRING_RESPONSE_STATE_CLAIMED =
      GetPairingResponseState._(
          2, _omitEnumNames ? '' : 'GET_PAIRING_RESPONSE_STATE_CLAIMED');
  static const GetPairingResponseState GET_PAIRING_RESPONSE_STATE_CONFIRMED =
      GetPairingResponseState._(
          3, _omitEnumNames ? '' : 'GET_PAIRING_RESPONSE_STATE_CONFIRMED');

  static const $core.List<GetPairingResponseState> values =
      <GetPairingResponseState>[
    GET_PAIRING_RESPONSE_STATE_UNSPECIFIED,
    GET_PAIRING_RESPONSE_STATE_WAITING,
    GET_PAIRING_RESPONSE_STATE_CLAIMED,
    GET_PAIRING_RESPONSE_STATE_CONFIRMED,
  ];

  static final $core.List<GetPairingResponseState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static GetPairingResponseState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const GetPairingResponseState._(super.value, super.name);
}

class UpdatePrefsRequestThreadView extends $pb.ProtobufEnum {
  static const UpdatePrefsRequestThreadView
      UPDATE_PREFS_REQUEST_THREAD_VIEW_UNSPECIFIED =
      UpdatePrefsRequestThreadView._(0,
          _omitEnumNames ? '' : 'UPDATE_PREFS_REQUEST_THREAD_VIEW_UNSPECIFIED');
  static const UpdatePrefsRequestThreadView
      UPDATE_PREFS_REQUEST_THREAD_VIEW_THREADED =
      UpdatePrefsRequestThreadView._(
          1, _omitEnumNames ? '' : 'UPDATE_PREFS_REQUEST_THREAD_VIEW_THREADED');
  static const UpdatePrefsRequestThreadView
      UPDATE_PREFS_REQUEST_THREAD_VIEW_FLAT = UpdatePrefsRequestThreadView._(
          2, _omitEnumNames ? '' : 'UPDATE_PREFS_REQUEST_THREAD_VIEW_FLAT');

  static const $core.List<UpdatePrefsRequestThreadView> values =
      <UpdatePrefsRequestThreadView>[
    UPDATE_PREFS_REQUEST_THREAD_VIEW_UNSPECIFIED,
    UPDATE_PREFS_REQUEST_THREAD_VIEW_THREADED,
    UPDATE_PREFS_REQUEST_THREAD_VIEW_FLAT,
  ];

  static final $core.List<UpdatePrefsRequestThreadView?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static UpdatePrefsRequestThreadView? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdatePrefsRequestThreadView._(super.value, super.name);
}

class DevicePlatform extends $pb.ProtobufEnum {
  static const DevicePlatform DEVICE_PLATFORM_UNSPECIFIED =
      DevicePlatform._(0, _omitEnumNames ? '' : 'DEVICE_PLATFORM_UNSPECIFIED');
  static const DevicePlatform DEVICE_PLATFORM_ANDROID =
      DevicePlatform._(1, _omitEnumNames ? '' : 'DEVICE_PLATFORM_ANDROID');
  static const DevicePlatform DEVICE_PLATFORM_IOS =
      DevicePlatform._(2, _omitEnumNames ? '' : 'DEVICE_PLATFORM_IOS');
  static const DevicePlatform DEVICE_PLATFORM_MACOS =
      DevicePlatform._(3, _omitEnumNames ? '' : 'DEVICE_PLATFORM_MACOS');
  static const DevicePlatform DEVICE_PLATFORM_WINDOWS =
      DevicePlatform._(4, _omitEnumNames ? '' : 'DEVICE_PLATFORM_WINDOWS');
  static const DevicePlatform DEVICE_PLATFORM_LINUX =
      DevicePlatform._(5, _omitEnumNames ? '' : 'DEVICE_PLATFORM_LINUX');
  static const DevicePlatform DEVICE_PLATFORM_WEB =
      DevicePlatform._(6, _omitEnumNames ? '' : 'DEVICE_PLATFORM_WEB');

  static const $core.List<DevicePlatform> values = <DevicePlatform>[
    DEVICE_PLATFORM_UNSPECIFIED,
    DEVICE_PLATFORM_ANDROID,
    DEVICE_PLATFORM_IOS,
    DEVICE_PLATFORM_MACOS,
    DEVICE_PLATFORM_WINDOWS,
    DEVICE_PLATFORM_LINUX,
    DEVICE_PLATFORM_WEB,
  ];

  static final $core.List<DevicePlatform?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static DevicePlatform? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DevicePlatform._(super.value, super.name);
}

class DevicePushKind extends $pb.ProtobufEnum {
  static const DevicePushKind DEVICE_PUSH_KIND_UNSPECIFIED =
      DevicePushKind._(0, _omitEnumNames ? '' : 'DEVICE_PUSH_KIND_UNSPECIFIED');
  static const DevicePushKind DEVICE_PUSH_KIND_APNS =
      DevicePushKind._(1, _omitEnumNames ? '' : 'DEVICE_PUSH_KIND_APNS');
  static const DevicePushKind DEVICE_PUSH_KIND_FCM =
      DevicePushKind._(2, _omitEnumNames ? '' : 'DEVICE_PUSH_KIND_FCM');
  static const DevicePushKind DEVICE_PUSH_KIND_UNIFIEDPUSH =
      DevicePushKind._(3, _omitEnumNames ? '' : 'DEVICE_PUSH_KIND_UNIFIEDPUSH');
  static const DevicePushKind DEVICE_PUSH_KIND_NONE =
      DevicePushKind._(4, _omitEnumNames ? '' : 'DEVICE_PUSH_KIND_NONE');

  static const $core.List<DevicePushKind> values = <DevicePushKind>[
    DEVICE_PUSH_KIND_UNSPECIFIED,
    DEVICE_PUSH_KIND_APNS,
    DEVICE_PUSH_KIND_FCM,
    DEVICE_PUSH_KIND_UNIFIEDPUSH,
    DEVICE_PUSH_KIND_NONE,
  ];

  static final $core.List<DevicePushKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static DevicePushKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DevicePushKind._(super.value, super.name);
}

class DeviceInputPlatform extends $pb.ProtobufEnum {
  static const DeviceInputPlatform DEVICE_INPUT_PLATFORM_UNSPECIFIED =
      DeviceInputPlatform._(
          0, _omitEnumNames ? '' : 'DEVICE_INPUT_PLATFORM_UNSPECIFIED');
  static const DeviceInputPlatform DEVICE_INPUT_PLATFORM_ANDROID =
      DeviceInputPlatform._(
          1, _omitEnumNames ? '' : 'DEVICE_INPUT_PLATFORM_ANDROID');
  static const DeviceInputPlatform DEVICE_INPUT_PLATFORM_IOS =
      DeviceInputPlatform._(
          2, _omitEnumNames ? '' : 'DEVICE_INPUT_PLATFORM_IOS');
  static const DeviceInputPlatform DEVICE_INPUT_PLATFORM_MACOS =
      DeviceInputPlatform._(
          3, _omitEnumNames ? '' : 'DEVICE_INPUT_PLATFORM_MACOS');
  static const DeviceInputPlatform DEVICE_INPUT_PLATFORM_WINDOWS =
      DeviceInputPlatform._(
          4, _omitEnumNames ? '' : 'DEVICE_INPUT_PLATFORM_WINDOWS');
  static const DeviceInputPlatform DEVICE_INPUT_PLATFORM_LINUX =
      DeviceInputPlatform._(
          5, _omitEnumNames ? '' : 'DEVICE_INPUT_PLATFORM_LINUX');
  static const DeviceInputPlatform DEVICE_INPUT_PLATFORM_WEB =
      DeviceInputPlatform._(
          6, _omitEnumNames ? '' : 'DEVICE_INPUT_PLATFORM_WEB');

  static const $core.List<DeviceInputPlatform> values = <DeviceInputPlatform>[
    DEVICE_INPUT_PLATFORM_UNSPECIFIED,
    DEVICE_INPUT_PLATFORM_ANDROID,
    DEVICE_INPUT_PLATFORM_IOS,
    DEVICE_INPUT_PLATFORM_MACOS,
    DEVICE_INPUT_PLATFORM_WINDOWS,
    DEVICE_INPUT_PLATFORM_LINUX,
    DEVICE_INPUT_PLATFORM_WEB,
  ];

  static final $core.List<DeviceInputPlatform?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static DeviceInputPlatform? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DeviceInputPlatform._(super.value, super.name);
}

class DeviceInputPushKind extends $pb.ProtobufEnum {
  static const DeviceInputPushKind DEVICE_INPUT_PUSH_KIND_UNSPECIFIED =
      DeviceInputPushKind._(
          0, _omitEnumNames ? '' : 'DEVICE_INPUT_PUSH_KIND_UNSPECIFIED');
  static const DeviceInputPushKind DEVICE_INPUT_PUSH_KIND_APNS =
      DeviceInputPushKind._(
          1, _omitEnumNames ? '' : 'DEVICE_INPUT_PUSH_KIND_APNS');
  static const DeviceInputPushKind DEVICE_INPUT_PUSH_KIND_FCM =
      DeviceInputPushKind._(
          2, _omitEnumNames ? '' : 'DEVICE_INPUT_PUSH_KIND_FCM');
  static const DeviceInputPushKind DEVICE_INPUT_PUSH_KIND_UNIFIEDPUSH =
      DeviceInputPushKind._(
          3, _omitEnumNames ? '' : 'DEVICE_INPUT_PUSH_KIND_UNIFIEDPUSH');
  static const DeviceInputPushKind DEVICE_INPUT_PUSH_KIND_NONE =
      DeviceInputPushKind._(
          4, _omitEnumNames ? '' : 'DEVICE_INPUT_PUSH_KIND_NONE');

  static const $core.List<DeviceInputPushKind> values = <DeviceInputPushKind>[
    DEVICE_INPUT_PUSH_KIND_UNSPECIFIED,
    DEVICE_INPUT_PUSH_KIND_APNS,
    DEVICE_INPUT_PUSH_KIND_FCM,
    DEVICE_INPUT_PUSH_KIND_UNIFIEDPUSH,
    DEVICE_INPUT_PUSH_KIND_NONE,
  ];

  static final $core.List<DeviceInputPushKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static DeviceInputPushKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DeviceInputPushKind._(super.value, super.name);
}

class WakeUpKind extends $pb.ProtobufEnum {
  static const WakeUpKind WAKE_UP_KIND_UNSPECIFIED =
      WakeUpKind._(0, _omitEnumNames ? '' : 'WAKE_UP_KIND_UNSPECIFIED');
  static const WakeUpKind WAKE_UP_KIND_MESSAGE =
      WakeUpKind._(1, _omitEnumNames ? '' : 'WAKE_UP_KIND_MESSAGE');
  static const WakeUpKind WAKE_UP_KIND_RING =
      WakeUpKind._(2, _omitEnumNames ? '' : 'WAKE_UP_KIND_RING');
  static const WakeUpKind WAKE_UP_KIND_REQUEST =
      WakeUpKind._(3, _omitEnumNames ? '' : 'WAKE_UP_KIND_REQUEST');

  static const $core.List<WakeUpKind> values = <WakeUpKind>[
    WAKE_UP_KIND_UNSPECIFIED,
    WAKE_UP_KIND_MESSAGE,
    WAKE_UP_KIND_RING,
    WAKE_UP_KIND_REQUEST,
  ];

  static final $core.List<WakeUpKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static WakeUpKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const WakeUpKind._(super.value, super.name);
}

class WakeUpConversationKind extends $pb.ProtobufEnum {
  static const WakeUpConversationKind WAKE_UP_CONVERSATION_KIND_UNSPECIFIED =
      WakeUpConversationKind._(
          0, _omitEnumNames ? '' : 'WAKE_UP_CONVERSATION_KIND_UNSPECIFIED');
  static const WakeUpConversationKind WAKE_UP_CONVERSATION_KIND_CHANNEL =
      WakeUpConversationKind._(
          1, _omitEnumNames ? '' : 'WAKE_UP_CONVERSATION_KIND_CHANNEL');
  static const WakeUpConversationKind WAKE_UP_CONVERSATION_KIND_SEALED =
      WakeUpConversationKind._(
          2, _omitEnumNames ? '' : 'WAKE_UP_CONVERSATION_KIND_SEALED');

  static const $core.List<WakeUpConversationKind> values =
      <WakeUpConversationKind>[
    WAKE_UP_CONVERSATION_KIND_UNSPECIFIED,
    WAKE_UP_CONVERSATION_KIND_CHANNEL,
    WAKE_UP_CONVERSATION_KIND_SEALED,
  ];

  static final $core.List<WakeUpConversationKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static WakeUpConversationKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const WakeUpConversationKind._(super.value, super.name);
}

class EntityType extends $pb.ProtobufEnum {
  static const EntityType ENTITY_TYPE_UNSPECIFIED =
      EntityType._(0, _omitEnumNames ? '' : 'ENTITY_TYPE_UNSPECIFIED');
  static const EntityType ENTITY_TYPE_ACCOUNT =
      EntityType._(1, _omitEnumNames ? '' : 'ENTITY_TYPE_ACCOUNT');
  static const EntityType ENTITY_TYPE_GROUP =
      EntityType._(2, _omitEnumNames ? '' : 'ENTITY_TYPE_GROUP');
  static const EntityType ENTITY_TYPE_BROADCAST =
      EntityType._(3, _omitEnumNames ? '' : 'ENTITY_TYPE_BROADCAST');
  static const EntityType ENTITY_TYPE_CONVERSATION =
      EntityType._(4, _omitEnumNames ? '' : 'ENTITY_TYPE_CONVERSATION');
  static const EntityType ENTITY_TYPE_EMOJI =
      EntityType._(5, _omitEnumNames ? '' : 'ENTITY_TYPE_EMOJI');
  static const EntityType ENTITY_TYPE_LINK =
      EntityType._(6, _omitEnumNames ? '' : 'ENTITY_TYPE_LINK');
  static const EntityType ENTITY_TYPE_TASK =
      EntityType._(7, _omitEnumNames ? '' : 'ENTITY_TYPE_TASK');
  static const EntityType ENTITY_TYPE_INSTANT =
      EntityType._(8, _omitEnumNames ? '' : 'ENTITY_TYPE_INSTANT');
  static const EntityType ENTITY_TYPE_REFERENCE =
      EntityType._(9, _omitEnumNames ? '' : 'ENTITY_TYPE_REFERENCE');
  static const EntityType ENTITY_TYPE_MESSAGE_LINK =
      EntityType._(10, _omitEnumNames ? '' : 'ENTITY_TYPE_MESSAGE_LINK');
  static const EntityType ENTITY_TYPE_MEASURE =
      EntityType._(11, _omitEnumNames ? '' : 'ENTITY_TYPE_MEASURE');
  static const EntityType ENTITY_TYPE_COLOUR =
      EntityType._(12, _omitEnumNames ? '' : 'ENTITY_TYPE_COLOUR');
  static const EntityType ENTITY_TYPE_EVENT =
      EntityType._(13, _omitEnumNames ? '' : 'ENTITY_TYPE_EVENT');
  static const EntityType ENTITY_TYPE_PHONE =
      EntityType._(14, _omitEnumNames ? '' : 'ENTITY_TYPE_PHONE');
  static const EntityType ENTITY_TYPE_EMAIL =
      EntityType._(15, _omitEnumNames ? '' : 'ENTITY_TYPE_EMAIL');
  static const EntityType ENTITY_TYPE_GEO =
      EntityType._(16, _omitEnumNames ? '' : 'ENTITY_TYPE_GEO');
  static const EntityType ENTITY_TYPE_DICE =
      EntityType._(17, _omitEnumNames ? '' : 'ENTITY_TYPE_DICE');
  static const EntityType ENTITY_TYPE_NUMBER =
      EntityType._(18, _omitEnumNames ? '' : 'ENTITY_TYPE_NUMBER');
  static const EntityType ENTITY_TYPE_CODEPOINT =
      EntityType._(19, _omitEnumNames ? '' : 'ENTITY_TYPE_CODEPOINT');

  static const $core.List<EntityType> values = <EntityType>[
    ENTITY_TYPE_UNSPECIFIED,
    ENTITY_TYPE_ACCOUNT,
    ENTITY_TYPE_GROUP,
    ENTITY_TYPE_BROADCAST,
    ENTITY_TYPE_CONVERSATION,
    ENTITY_TYPE_EMOJI,
    ENTITY_TYPE_LINK,
    ENTITY_TYPE_TASK,
    ENTITY_TYPE_INSTANT,
    ENTITY_TYPE_REFERENCE,
    ENTITY_TYPE_MESSAGE_LINK,
    ENTITY_TYPE_MEASURE,
    ENTITY_TYPE_COLOUR,
    ENTITY_TYPE_EVENT,
    ENTITY_TYPE_PHONE,
    ENTITY_TYPE_EMAIL,
    ENTITY_TYPE_GEO,
    ENTITY_TYPE_DICE,
    ENTITY_TYPE_NUMBER,
    ENTITY_TYPE_CODEPOINT,
  ];

  static final $core.List<EntityType?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 19);
  static EntityType? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const EntityType._(super.value, super.name);
}

class EntityScope extends $pb.ProtobufEnum {
  static const EntityScope ENTITY_SCOPE_UNSPECIFIED =
      EntityScope._(0, _omitEnumNames ? '' : 'ENTITY_SCOPE_UNSPECIFIED');
  static const EntityScope ENTITY_SCOPE_CHANNEL =
      EntityScope._(1, _omitEnumNames ? '' : 'ENTITY_SCOPE_CHANNEL');
  static const EntityScope ENTITY_SCOPE_HERE =
      EntityScope._(2, _omitEnumNames ? '' : 'ENTITY_SCOPE_HERE');

  static const $core.List<EntityScope> values = <EntityScope>[
    ENTITY_SCOPE_UNSPECIFIED,
    ENTITY_SCOPE_CHANNEL,
    ENTITY_SCOPE_HERE,
  ];

  static final $core.List<EntityScope?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static EntityScope? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const EntityScope._(super.value, super.name);
}

class EntityPrecision extends $pb.ProtobufEnum {
  static const EntityPrecision ENTITY_PRECISION_UNSPECIFIED = EntityPrecision._(
      0, _omitEnumNames ? '' : 'ENTITY_PRECISION_UNSPECIFIED');
  static const EntityPrecision ENTITY_PRECISION_MINUTE =
      EntityPrecision._(1, _omitEnumNames ? '' : 'ENTITY_PRECISION_MINUTE');
  static const EntityPrecision ENTITY_PRECISION_SECOND =
      EntityPrecision._(2, _omitEnumNames ? '' : 'ENTITY_PRECISION_SECOND');

  static const $core.List<EntityPrecision> values = <EntityPrecision>[
    ENTITY_PRECISION_UNSPECIFIED,
    ENTITY_PRECISION_MINUTE,
    ENTITY_PRECISION_SECOND,
  ];

  static final $core.List<EntityPrecision?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static EntityPrecision? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const EntityPrecision._(super.value, super.name);
}

class DiceTermKeep extends $pb.ProtobufEnum {
  static const DiceTermKeep DICE_TERM_KEEP_UNSPECIFIED =
      DiceTermKeep._(0, _omitEnumNames ? '' : 'DICE_TERM_KEEP_UNSPECIFIED');
  static const DiceTermKeep DICE_TERM_KEEP_KH =
      DiceTermKeep._(1, _omitEnumNames ? '' : 'DICE_TERM_KEEP_KH');
  static const DiceTermKeep DICE_TERM_KEEP_KL =
      DiceTermKeep._(2, _omitEnumNames ? '' : 'DICE_TERM_KEEP_KL');
  static const DiceTermKeep DICE_TERM_KEEP_DH =
      DiceTermKeep._(3, _omitEnumNames ? '' : 'DICE_TERM_KEEP_DH');
  static const DiceTermKeep DICE_TERM_KEEP_DL =
      DiceTermKeep._(4, _omitEnumNames ? '' : 'DICE_TERM_KEEP_DL');

  static const $core.List<DiceTermKeep> values = <DiceTermKeep>[
    DICE_TERM_KEEP_UNSPECIFIED,
    DICE_TERM_KEEP_KH,
    DICE_TERM_KEEP_KL,
    DICE_TERM_KEEP_DH,
    DICE_TERM_KEEP_DL,
  ];

  static final $core.List<DiceTermKeep?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static DiceTermKeep? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DiceTermKeep._(super.value, super.name);
}

class FileKind extends $pb.ProtobufEnum {
  static const FileKind FILE_KIND_UNSPECIFIED =
      FileKind._(0, _omitEnumNames ? '' : 'FILE_KIND_UNSPECIFIED');
  static const FileKind FILE_KIND_IMAGE =
      FileKind._(1, _omitEnumNames ? '' : 'FILE_KIND_IMAGE');
  static const FileKind FILE_KIND_VIDEO =
      FileKind._(2, _omitEnumNames ? '' : 'FILE_KIND_VIDEO');
  static const FileKind FILE_KIND_AUDIO =
      FileKind._(3, _omitEnumNames ? '' : 'FILE_KIND_AUDIO');
  static const FileKind FILE_KIND_TEXT =
      FileKind._(4, _omitEnumNames ? '' : 'FILE_KIND_TEXT');
  static const FileKind FILE_KIND_FILE =
      FileKind._(5, _omitEnumNames ? '' : 'FILE_KIND_FILE');

  static const $core.List<FileKind> values = <FileKind>[
    FILE_KIND_UNSPECIFIED,
    FILE_KIND_IMAGE,
    FILE_KIND_VIDEO,
    FILE_KIND_AUDIO,
    FILE_KIND_TEXT,
    FILE_KIND_FILE,
  ];

  static final $core.List<FileKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static FileKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FileKind._(super.value, super.name);
}

class FileState extends $pb.ProtobufEnum {
  static const FileState FILE_STATE_UNSPECIFIED =
      FileState._(0, _omitEnumNames ? '' : 'FILE_STATE_UNSPECIFIED');
  static const FileState FILE_STATE_READY =
      FileState._(1, _omitEnumNames ? '' : 'FILE_STATE_READY');
  static const FileState FILE_STATE_ARRIVING =
      FileState._(2, _omitEnumNames ? '' : 'FILE_STATE_ARRIVING');
  static const FileState FILE_STATE_UNAVAILABLE =
      FileState._(3, _omitEnumNames ? '' : 'FILE_STATE_UNAVAILABLE');
  static const FileState FILE_STATE_TOO_LARGE =
      FileState._(4, _omitEnumNames ? '' : 'FILE_STATE_TOO_LARGE');

  static const $core.List<FileState> values = <FileState>[
    FILE_STATE_UNSPECIFIED,
    FILE_STATE_READY,
    FILE_STATE_ARRIVING,
    FILE_STATE_UNAVAILABLE,
    FILE_STATE_TOO_LARGE,
  ];

  static final $core.List<FileState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static FileState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FileState._(super.value, super.name);
}

class CardColor extends $pb.ProtobufEnum {
  static const CardColor CARD_COLOR_UNSPECIFIED =
      CardColor._(0, _omitEnumNames ? '' : 'CARD_COLOR_UNSPECIFIED');
  static const CardColor CARD_COLOR_LAVENDER =
      CardColor._(1, _omitEnumNames ? '' : 'CARD_COLOR_LAVENDER');
  static const CardColor CARD_COLOR_CYAN =
      CardColor._(2, _omitEnumNames ? '' : 'CARD_COLOR_CYAN');
  static const CardColor CARD_COLOR_PINK =
      CardColor._(3, _omitEnumNames ? '' : 'CARD_COLOR_PINK');
  static const CardColor CARD_COLOR_YELLOW =
      CardColor._(4, _omitEnumNames ? '' : 'CARD_COLOR_YELLOW');
  static const CardColor CARD_COLOR_GREEN =
      CardColor._(5, _omitEnumNames ? '' : 'CARD_COLOR_GREEN');
  static const CardColor CARD_COLOR_RED =
      CardColor._(6, _omitEnumNames ? '' : 'CARD_COLOR_RED');
  static const CardColor CARD_COLOR_GREY =
      CardColor._(7, _omitEnumNames ? '' : 'CARD_COLOR_GREY');

  static const $core.List<CardColor> values = <CardColor>[
    CARD_COLOR_UNSPECIFIED,
    CARD_COLOR_LAVENDER,
    CARD_COLOR_CYAN,
    CARD_COLOR_PINK,
    CARD_COLOR_YELLOW,
    CARD_COLOR_GREEN,
    CARD_COLOR_RED,
    CARD_COLOR_GREY,
  ];

  static final $core.List<CardColor?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static CardColor? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CardColor._(super.value, super.name);
}

class FormPreset extends $pb.ProtobufEnum {
  static const FormPreset FORM_PRESET_UNSPECIFIED =
      FormPreset._(0, _omitEnumNames ? '' : 'FORM_PRESET_UNSPECIFIED');
  static const FormPreset FORM_PRESET_POLL =
      FormPreset._(1, _omitEnumNames ? '' : 'FORM_PRESET_POLL');
  static const FormPreset FORM_PRESET_RSVP =
      FormPreset._(2, _omitEnumNames ? '' : 'FORM_PRESET_RSVP');
  static const FormPreset FORM_PRESET_SIGNUP =
      FormPreset._(3, _omitEnumNames ? '' : 'FORM_PRESET_SIGNUP');
  static const FormPreset FORM_PRESET_APPROVE =
      FormPreset._(4, _omitEnumNames ? '' : 'FORM_PRESET_APPROVE');
  static const FormPreset FORM_PRESET_CHECKIN =
      FormPreset._(5, _omitEnumNames ? '' : 'FORM_PRESET_CHECKIN');
  static const FormPreset FORM_PRESET_TICKET =
      FormPreset._(6, _omitEnumNames ? '' : 'FORM_PRESET_TICKET');
  static const FormPreset FORM_PRESET_ROLES =
      FormPreset._(7, _omitEnumNames ? '' : 'FORM_PRESET_ROLES');

  static const $core.List<FormPreset> values = <FormPreset>[
    FORM_PRESET_UNSPECIFIED,
    FORM_PRESET_POLL,
    FORM_PRESET_RSVP,
    FORM_PRESET_SIGNUP,
    FORM_PRESET_APPROVE,
    FORM_PRESET_CHECKIN,
    FORM_PRESET_TICKET,
    FORM_PRESET_ROLES,
  ];

  static final $core.List<FormPreset?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static FormPreset? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FormPreset._(super.value, super.name);
}

class FormFieldType extends $pb.ProtobufEnum {
  static const FormFieldType FORM_FIELD_TYPE_UNSPECIFIED =
      FormFieldType._(0, _omitEnumNames ? '' : 'FORM_FIELD_TYPE_UNSPECIFIED');
  static const FormFieldType FORM_FIELD_TYPE_CHOICE =
      FormFieldType._(1, _omitEnumNames ? '' : 'FORM_FIELD_TYPE_CHOICE');
  static const FormFieldType FORM_FIELD_TYPE_TEXT =
      FormFieldType._(2, _omitEnumNames ? '' : 'FORM_FIELD_TYPE_TEXT');
  static const FormFieldType FORM_FIELD_TYPE_LONGTEXT =
      FormFieldType._(3, _omitEnumNames ? '' : 'FORM_FIELD_TYPE_LONGTEXT');
  static const FormFieldType FORM_FIELD_TYPE_NUMBER =
      FormFieldType._(4, _omitEnumNames ? '' : 'FORM_FIELD_TYPE_NUMBER');
  static const FormFieldType FORM_FIELD_TYPE_DATE =
      FormFieldType._(5, _omitEnumNames ? '' : 'FORM_FIELD_TYPE_DATE');
  static const FormFieldType FORM_FIELD_TYPE_TIME =
      FormFieldType._(6, _omitEnumNames ? '' : 'FORM_FIELD_TYPE_TIME');
  static const FormFieldType FORM_FIELD_TYPE_ACCOUNT =
      FormFieldType._(7, _omitEnumNames ? '' : 'FORM_FIELD_TYPE_ACCOUNT');
  static const FormFieldType FORM_FIELD_TYPE_CONVERSATION =
      FormFieldType._(8, _omitEnumNames ? '' : 'FORM_FIELD_TYPE_CONVERSATION');
  static const FormFieldType FORM_FIELD_TYPE_CHECKBOX =
      FormFieldType._(9, _omitEnumNames ? '' : 'FORM_FIELD_TYPE_CHECKBOX');

  static const $core.List<FormFieldType> values = <FormFieldType>[
    FORM_FIELD_TYPE_UNSPECIFIED,
    FORM_FIELD_TYPE_CHOICE,
    FORM_FIELD_TYPE_TEXT,
    FORM_FIELD_TYPE_LONGTEXT,
    FORM_FIELD_TYPE_NUMBER,
    FORM_FIELD_TYPE_DATE,
    FORM_FIELD_TYPE_TIME,
    FORM_FIELD_TYPE_ACCOUNT,
    FORM_FIELD_TYPE_CONVERSATION,
    FORM_FIELD_TYPE_CHECKBOX,
  ];

  static final $core.List<FormFieldType?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 9);
  static FormFieldType? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FormFieldType._(super.value, super.name);
}

class FormButtonStyle extends $pb.ProtobufEnum {
  static const FormButtonStyle FORM_BUTTON_STYLE_UNSPECIFIED =
      FormButtonStyle._(
          0, _omitEnumNames ? '' : 'FORM_BUTTON_STYLE_UNSPECIFIED');
  static const FormButtonStyle FORM_BUTTON_STYLE_DEFAULT =
      FormButtonStyle._(1, _omitEnumNames ? '' : 'FORM_BUTTON_STYLE_DEFAULT');
  static const FormButtonStyle FORM_BUTTON_STYLE_PRIMARY =
      FormButtonStyle._(2, _omitEnumNames ? '' : 'FORM_BUTTON_STYLE_PRIMARY');
  static const FormButtonStyle FORM_BUTTON_STYLE_DANGER =
      FormButtonStyle._(3, _omitEnumNames ? '' : 'FORM_BUTTON_STYLE_DANGER');

  static const $core.List<FormButtonStyle> values = <FormButtonStyle>[
    FORM_BUTTON_STYLE_UNSPECIFIED,
    FORM_BUTTON_STYLE_DEFAULT,
    FORM_BUTTON_STYLE_PRIMARY,
    FORM_BUTTON_STYLE_DANGER,
  ];

  static final $core.List<FormButtonStyle?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static FormButtonStyle? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FormButtonStyle._(super.value, super.name);
}

class FormButtonGroupAction extends $pb.ProtobufEnum {
  static const FormButtonGroupAction FORM_BUTTON_GROUP_ACTION_UNSPECIFIED =
      FormButtonGroupAction._(
          0, _omitEnumNames ? '' : 'FORM_BUTTON_GROUP_ACTION_UNSPECIFIED');
  static const FormButtonGroupAction FORM_BUTTON_GROUP_ACTION_JOIN =
      FormButtonGroupAction._(
          1, _omitEnumNames ? '' : 'FORM_BUTTON_GROUP_ACTION_JOIN');
  static const FormButtonGroupAction FORM_BUTTON_GROUP_ACTION_LEAVE =
      FormButtonGroupAction._(
          2, _omitEnumNames ? '' : 'FORM_BUTTON_GROUP_ACTION_LEAVE');

  static const $core.List<FormButtonGroupAction> values =
      <FormButtonGroupAction>[
    FORM_BUTTON_GROUP_ACTION_UNSPECIFIED,
    FORM_BUTTON_GROUP_ACTION_JOIN,
    FORM_BUTTON_GROUP_ACTION_LEAVE,
  ];

  static final $core.List<FormButtonGroupAction?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static FormButtonGroupAction? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FormButtonGroupAction._(super.value, super.name);
}

class FormActionType extends $pb.ProtobufEnum {
  static const FormActionType FORM_ACTION_TYPE_UNSPECIFIED =
      FormActionType._(0, _omitEnumNames ? '' : 'FORM_ACTION_TYPE_UNSPECIFIED');
  static const FormActionType FORM_ACTION_TYPE_TALLY =
      FormActionType._(1, _omitEnumNames ? '' : 'FORM_ACTION_TYPE_TALLY');
  static const FormActionType FORM_ACTION_TYPE_COLLECT =
      FormActionType._(2, _omitEnumNames ? '' : 'FORM_ACTION_TYPE_COLLECT');
  static const FormActionType FORM_ACTION_TYPE_THREAD =
      FormActionType._(3, _omitEnumNames ? '' : 'FORM_ACTION_TYPE_THREAD');
  static const FormActionType FORM_ACTION_TYPE_CHANNEL =
      FormActionType._(4, _omitEnumNames ? '' : 'FORM_ACTION_TYPE_CHANNEL');
  static const FormActionType FORM_ACTION_TYPE_GROUP =
      FormActionType._(5, _omitEnumNames ? '' : 'FORM_ACTION_TYPE_GROUP');
  static const FormActionType FORM_ACTION_TYPE_BOT =
      FormActionType._(6, _omitEnumNames ? '' : 'FORM_ACTION_TYPE_BOT');

  static const $core.List<FormActionType> values = <FormActionType>[
    FORM_ACTION_TYPE_UNSPECIFIED,
    FORM_ACTION_TYPE_TALLY,
    FORM_ACTION_TYPE_COLLECT,
    FORM_ACTION_TYPE_THREAD,
    FORM_ACTION_TYPE_CHANNEL,
    FORM_ACTION_TYPE_GROUP,
    FORM_ACTION_TYPE_BOT,
  ];

  static final $core.List<FormActionType?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static FormActionType? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FormActionType._(super.value, super.name);
}

class FormActionLevel extends $pb.ProtobufEnum {
  static const FormActionLevel FORM_ACTION_LEVEL_UNSPECIFIED =
      FormActionLevel._(
          0, _omitEnumNames ? '' : 'FORM_ACTION_LEVEL_UNSPECIFIED');
  static const FormActionLevel FORM_ACTION_LEVEL_READER =
      FormActionLevel._(1, _omitEnumNames ? '' : 'FORM_ACTION_LEVEL_READER');
  static const FormActionLevel FORM_ACTION_LEVEL_MEMBER =
      FormActionLevel._(2, _omitEnumNames ? '' : 'FORM_ACTION_LEVEL_MEMBER');
  static const FormActionLevel FORM_ACTION_LEVEL_MANAGER =
      FormActionLevel._(3, _omitEnumNames ? '' : 'FORM_ACTION_LEVEL_MANAGER');

  static const $core.List<FormActionLevel> values = <FormActionLevel>[
    FORM_ACTION_LEVEL_UNSPECIFIED,
    FORM_ACTION_LEVEL_READER,
    FORM_ACTION_LEVEL_MEMBER,
    FORM_ACTION_LEVEL_MANAGER,
  ];

  static final $core.List<FormActionLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static FormActionLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FormActionLevel._(super.value, super.name);
}

class FormResults extends $pb.ProtobufEnum {
  static const FormResults FORM_RESULTS_UNSPECIFIED =
      FormResults._(0, _omitEnumNames ? '' : 'FORM_RESULTS_UNSPECIFIED');
  static const FormResults FORM_RESULTS_LIVE =
      FormResults._(1, _omitEnumNames ? '' : 'FORM_RESULTS_LIVE');
  static const FormResults FORM_RESULTS_AFTER_ANSWER =
      FormResults._(2, _omitEnumNames ? '' : 'FORM_RESULTS_AFTER_ANSWER');
  static const FormResults FORM_RESULTS_AFTER_CLOSE =
      FormResults._(3, _omitEnumNames ? '' : 'FORM_RESULTS_AFTER_CLOSE');

  static const $core.List<FormResults> values = <FormResults>[
    FORM_RESULTS_UNSPECIFIED,
    FORM_RESULTS_LIVE,
    FORM_RESULTS_AFTER_ANSWER,
    FORM_RESULTS_AFTER_CLOSE,
  ];

  static final $core.List<FormResults?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static FormResults? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FormResults._(super.value, super.name);
}

class ChanceKind extends $pb.ProtobufEnum {
  static const ChanceKind CHANCE_KIND_UNSPECIFIED =
      ChanceKind._(0, _omitEnumNames ? '' : 'CHANCE_KIND_UNSPECIFIED');
  static const ChanceKind CHANCE_KIND_ROLL =
      ChanceKind._(1, _omitEnumNames ? '' : 'CHANCE_KIND_ROLL');
  static const ChanceKind CHANCE_KIND_FLIP =
      ChanceKind._(2, _omitEnumNames ? '' : 'CHANCE_KIND_FLIP');
  static const ChanceKind CHANCE_KIND_PICK =
      ChanceKind._(3, _omitEnumNames ? '' : 'CHANCE_KIND_PICK');
  static const ChanceKind CHANCE_KIND_SHUFFLE =
      ChanceKind._(4, _omitEnumNames ? '' : 'CHANCE_KIND_SHUFFLE');
  static const ChanceKind CHANCE_KIND_DRAW =
      ChanceKind._(5, _omitEnumNames ? '' : 'CHANCE_KIND_DRAW');
  static const ChanceKind CHANCE_KIND_TAROT =
      ChanceKind._(6, _omitEnumNames ? '' : 'CHANCE_KIND_TAROT');

  static const $core.List<ChanceKind> values = <ChanceKind>[
    CHANCE_KIND_UNSPECIFIED,
    CHANCE_KIND_ROLL,
    CHANCE_KIND_FLIP,
    CHANCE_KIND_PICK,
    CHANCE_KIND_SHUFFLE,
    CHANCE_KIND_DRAW,
    CHANCE_KIND_TAROT,
  ];

  static final $core.List<ChanceKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static ChanceKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ChanceKind._(super.value, super.name);
}

class ChanceOutcome extends $pb.ProtobufEnum {
  static const ChanceOutcome CHANCE_OUTCOME_UNSPECIFIED =
      ChanceOutcome._(0, _omitEnumNames ? '' : 'CHANCE_OUTCOME_UNSPECIFIED');
  static const ChanceOutcome CHANCE_OUTCOME_HEADS =
      ChanceOutcome._(1, _omitEnumNames ? '' : 'CHANCE_OUTCOME_HEADS');
  static const ChanceOutcome CHANCE_OUTCOME_TAILS =
      ChanceOutcome._(2, _omitEnumNames ? '' : 'CHANCE_OUTCOME_TAILS');

  static const $core.List<ChanceOutcome> values = <ChanceOutcome>[
    CHANCE_OUTCOME_UNSPECIFIED,
    CHANCE_OUTCOME_HEADS,
    CHANCE_OUTCOME_TAILS,
  ];

  static final $core.List<ChanceOutcome?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ChanceOutcome? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ChanceOutcome._(super.value, super.name);
}

class ViaKind extends $pb.ProtobufEnum {
  static const ViaKind VIA_KIND_UNSPECIFIED =
      ViaKind._(0, _omitEnumNames ? '' : 'VIA_KIND_UNSPECIFIED');
  static const ViaKind VIA_KIND_WEBHOOK =
      ViaKind._(1, _omitEnumNames ? '' : 'VIA_KIND_WEBHOOK');
  static const ViaKind VIA_KIND_REMINDER =
      ViaKind._(2, _omitEnumNames ? '' : 'VIA_KIND_REMINDER');
  static const ViaKind VIA_KIND_SCHEDULED =
      ViaKind._(3, _omitEnumNames ? '' : 'VIA_KIND_SCHEDULED');
  static const ViaKind VIA_KIND_COMMAND =
      ViaKind._(4, _omitEnumNames ? '' : 'VIA_KIND_COMMAND');
  static const ViaKind VIA_KIND_IMPORT =
      ViaKind._(5, _omitEnumNames ? '' : 'VIA_KIND_IMPORT');
  static const ViaKind VIA_KIND_SYSTEM =
      ViaKind._(6, _omitEnumNames ? '' : 'VIA_KIND_SYSTEM');

  static const $core.List<ViaKind> values = <ViaKind>[
    VIA_KIND_UNSPECIFIED,
    VIA_KIND_WEBHOOK,
    VIA_KIND_REMINDER,
    VIA_KIND_SCHEDULED,
    VIA_KIND_COMMAND,
    VIA_KIND_IMPORT,
    VIA_KIND_SYSTEM,
  ];

  static final $core.List<ViaKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static ViaKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ViaKind._(super.value, super.name);
}

class SystemNoticeKind extends $pb.ProtobufEnum {
  static const SystemNoticeKind SYSTEM_NOTICE_KIND_UNSPECIFIED =
      SystemNoticeKind._(
          0, _omitEnumNames ? '' : 'SYSTEM_NOTICE_KIND_UNSPECIFIED');
  static const SystemNoticeKind SYSTEM_NOTICE_KIND_MEMBER_JOINED =
      SystemNoticeKind._(
          1, _omitEnumNames ? '' : 'SYSTEM_NOTICE_KIND_MEMBER_JOINED');
  static const SystemNoticeKind SYSTEM_NOTICE_KIND_MEMBER_LEFT =
      SystemNoticeKind._(
          2, _omitEnumNames ? '' : 'SYSTEM_NOTICE_KIND_MEMBER_LEFT');
  static const SystemNoticeKind SYSTEM_NOTICE_KIND_CALL_STARTED =
      SystemNoticeKind._(
          3, _omitEnumNames ? '' : 'SYSTEM_NOTICE_KIND_CALL_STARTED');
  static const SystemNoticeKind SYSTEM_NOTICE_KIND_CALL_ENDED =
      SystemNoticeKind._(
          4, _omitEnumNames ? '' : 'SYSTEM_NOTICE_KIND_CALL_ENDED');
  static const SystemNoticeKind SYSTEM_NOTICE_KIND_THREAD_MOVED =
      SystemNoticeKind._(
          5, _omitEnumNames ? '' : 'SYSTEM_NOTICE_KIND_THREAD_MOVED');
  static const SystemNoticeKind SYSTEM_NOTICE_KIND_CHANNEL_RENAMED =
      SystemNoticeKind._(
          6, _omitEnumNames ? '' : 'SYSTEM_NOTICE_KIND_CHANNEL_RENAMED');
  static const SystemNoticeKind SYSTEM_NOTICE_KIND_PINNED =
      SystemNoticeKind._(7, _omitEnumNames ? '' : 'SYSTEM_NOTICE_KIND_PINNED');

  static const $core.List<SystemNoticeKind> values = <SystemNoticeKind>[
    SYSTEM_NOTICE_KIND_UNSPECIFIED,
    SYSTEM_NOTICE_KIND_MEMBER_JOINED,
    SYSTEM_NOTICE_KIND_MEMBER_LEFT,
    SYSTEM_NOTICE_KIND_CALL_STARTED,
    SYSTEM_NOTICE_KIND_CALL_ENDED,
    SYSTEM_NOTICE_KIND_THREAD_MOVED,
    SYSTEM_NOTICE_KIND_CHANNEL_RENAMED,
    SYSTEM_NOTICE_KIND_PINNED,
  ];

  static final $core.List<SystemNoticeKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static SystemNoticeKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SystemNoticeKind._(super.value, super.name);
}

class WakeUpRequestKind extends $pb.ProtobufEnum {
  static const WakeUpRequestKind WAKE_UP_REQUEST_KIND_UNSPECIFIED =
      WakeUpRequestKind._(
          0, _omitEnumNames ? '' : 'WAKE_UP_REQUEST_KIND_UNSPECIFIED');
  static const WakeUpRequestKind WAKE_UP_REQUEST_KIND_DIRECT =
      WakeUpRequestKind._(
          1, _omitEnumNames ? '' : 'WAKE_UP_REQUEST_KIND_DIRECT');
  static const WakeUpRequestKind WAKE_UP_REQUEST_KIND_SEALED =
      WakeUpRequestKind._(
          2, _omitEnumNames ? '' : 'WAKE_UP_REQUEST_KIND_SEALED');

  static const $core.List<WakeUpRequestKind> values = <WakeUpRequestKind>[
    WAKE_UP_REQUEST_KIND_UNSPECIFIED,
    WAKE_UP_REQUEST_KIND_DIRECT,
    WAKE_UP_REQUEST_KIND_SEALED,
  ];

  static final $core.List<WakeUpRequestKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static WakeUpRequestKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const WakeUpRequestKind._(super.value, super.name);
}

class SetAvailabilityRequestValue extends $pb.ProtobufEnum {
  static const SetAvailabilityRequestValue
      SET_AVAILABILITY_REQUEST_VALUE_UNSPECIFIED =
      SetAvailabilityRequestValue._(0,
          _omitEnumNames ? '' : 'SET_AVAILABILITY_REQUEST_VALUE_UNSPECIFIED');
  static const SetAvailabilityRequestValue
      SET_AVAILABILITY_REQUEST_VALUE_MEETING = SetAvailabilityRequestValue._(
          1, _omitEnumNames ? '' : 'SET_AVAILABILITY_REQUEST_VALUE_MEETING');
  static const SetAvailabilityRequestValue
      SET_AVAILABILITY_REQUEST_VALUE_OUT_OF_OFFICE =
      SetAvailabilityRequestValue._(2,
          _omitEnumNames ? '' : 'SET_AVAILABILITY_REQUEST_VALUE_OUT_OF_OFFICE');

  static const $core.List<SetAvailabilityRequestValue> values =
      <SetAvailabilityRequestValue>[
    SET_AVAILABILITY_REQUEST_VALUE_UNSPECIFIED,
    SET_AVAILABILITY_REQUEST_VALUE_MEETING,
    SET_AVAILABILITY_REQUEST_VALUE_OUT_OF_OFFICE,
  ];

  static final $core.List<SetAvailabilityRequestValue?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static SetAvailabilityRequestValue? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SetAvailabilityRequestValue._(super.value, super.name);
}

class SidebarGroupSort extends $pb.ProtobufEnum {
  static const SidebarGroupSort SIDEBAR_GROUP_SORT_UNSPECIFIED =
      SidebarGroupSort._(
          0, _omitEnumNames ? '' : 'SIDEBAR_GROUP_SORT_UNSPECIFIED');
  static const SidebarGroupSort SIDEBAR_GROUP_SORT_ALPHABETICAL =
      SidebarGroupSort._(
          1, _omitEnumNames ? '' : 'SIDEBAR_GROUP_SORT_ALPHABETICAL');
  static const SidebarGroupSort SIDEBAR_GROUP_SORT_ACTIVITY =
      SidebarGroupSort._(
          2, _omitEnumNames ? '' : 'SIDEBAR_GROUP_SORT_ACTIVITY');
  static const SidebarGroupSort SIDEBAR_GROUP_SORT_CUSTOM =
      SidebarGroupSort._(3, _omitEnumNames ? '' : 'SIDEBAR_GROUP_SORT_CUSTOM');

  static const $core.List<SidebarGroupSort> values = <SidebarGroupSort>[
    SIDEBAR_GROUP_SORT_UNSPECIFIED,
    SIDEBAR_GROUP_SORT_ALPHABETICAL,
    SIDEBAR_GROUP_SORT_ACTIVITY,
    SIDEBAR_GROUP_SORT_CUSTOM,
  ];

  static final $core.List<SidebarGroupSort?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static SidebarGroupSort? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SidebarGroupSort._(super.value, super.name);
}

class SidebarOpOp extends $pb.ProtobufEnum {
  static const SidebarOpOp SIDEBAR_OP_OP_UNSPECIFIED =
      SidebarOpOp._(0, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_UNSPECIFIED');
  static const SidebarOpOp SIDEBAR_OP_OP_CREATE_SECTION =
      SidebarOpOp._(1, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_CREATE_SECTION');
  static const SidebarOpOp SIDEBAR_OP_OP_RENAME_SECTION =
      SidebarOpOp._(2, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_RENAME_SECTION');
  static const SidebarOpOp SIDEBAR_OP_OP_DELETE_SECTION =
      SidebarOpOp._(3, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_DELETE_SECTION');
  static const SidebarOpOp SIDEBAR_OP_OP_MOVE_SECTION =
      SidebarOpOp._(4, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_MOVE_SECTION');
  static const SidebarOpOp SIDEBAR_OP_OP_SET_SORT =
      SidebarOpOp._(5, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_SET_SORT');
  static const SidebarOpOp SIDEBAR_OP_OP_REORDER =
      SidebarOpOp._(6, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_REORDER');
  static const SidebarOpOp SIDEBAR_OP_OP_RESET_SECTION =
      SidebarOpOp._(7, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_RESET_SECTION');
  static const SidebarOpOp SIDEBAR_OP_OP_RESET_ALL =
      SidebarOpOp._(8, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_RESET_ALL');
  static const SidebarOpOp SIDEBAR_OP_OP_MOVE =
      SidebarOpOp._(9, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_MOVE');
  static const SidebarOpOp SIDEBAR_OP_OP_ORDER_STARS =
      SidebarOpOp._(10, _omitEnumNames ? '' : 'SIDEBAR_OP_OP_ORDER_STARS');

  static const $core.List<SidebarOpOp> values = <SidebarOpOp>[
    SIDEBAR_OP_OP_UNSPECIFIED,
    SIDEBAR_OP_OP_CREATE_SECTION,
    SIDEBAR_OP_OP_RENAME_SECTION,
    SIDEBAR_OP_OP_DELETE_SECTION,
    SIDEBAR_OP_OP_MOVE_SECTION,
    SIDEBAR_OP_OP_SET_SORT,
    SIDEBAR_OP_OP_REORDER,
    SIDEBAR_OP_OP_RESET_SECTION,
    SIDEBAR_OP_OP_RESET_ALL,
    SIDEBAR_OP_OP_MOVE,
    SIDEBAR_OP_OP_ORDER_STARS,
  ];

  static final $core.List<SidebarOpOp?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 10);
  static SidebarOpOp? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SidebarOpOp._(super.value, super.name);
}

class SidebarOpSort extends $pb.ProtobufEnum {
  static const SidebarOpSort SIDEBAR_OP_SORT_UNSPECIFIED =
      SidebarOpSort._(0, _omitEnumNames ? '' : 'SIDEBAR_OP_SORT_UNSPECIFIED');
  static const SidebarOpSort SIDEBAR_OP_SORT_ALPHABETICAL =
      SidebarOpSort._(1, _omitEnumNames ? '' : 'SIDEBAR_OP_SORT_ALPHABETICAL');
  static const SidebarOpSort SIDEBAR_OP_SORT_ACTIVITY =
      SidebarOpSort._(2, _omitEnumNames ? '' : 'SIDEBAR_OP_SORT_ACTIVITY');
  static const SidebarOpSort SIDEBAR_OP_SORT_CUSTOM =
      SidebarOpSort._(3, _omitEnumNames ? '' : 'SIDEBAR_OP_SORT_CUSTOM');

  static const $core.List<SidebarOpSort> values = <SidebarOpSort>[
    SIDEBAR_OP_SORT_UNSPECIFIED,
    SIDEBAR_OP_SORT_ALPHABETICAL,
    SIDEBAR_OP_SORT_ACTIVITY,
    SIDEBAR_OP_SORT_CUSTOM,
  ];

  static final $core.List<SidebarOpSort?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static SidebarOpSort? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SidebarOpSort._(super.value, super.name);
}

class StatusPresetAvailability extends $pb.ProtobufEnum {
  static const StatusPresetAvailability STATUS_PRESET_AVAILABILITY_UNSPECIFIED =
      StatusPresetAvailability._(
          0, _omitEnumNames ? '' : 'STATUS_PRESET_AVAILABILITY_UNSPECIFIED');
  static const StatusPresetAvailability STATUS_PRESET_AVAILABILITY_MEETING =
      StatusPresetAvailability._(
          1, _omitEnumNames ? '' : 'STATUS_PRESET_AVAILABILITY_MEETING');
  static const StatusPresetAvailability
      STATUS_PRESET_AVAILABILITY_OUT_OF_OFFICE = StatusPresetAvailability._(
          2, _omitEnumNames ? '' : 'STATUS_PRESET_AVAILABILITY_OUT_OF_OFFICE');

  static const $core.List<StatusPresetAvailability> values =
      <StatusPresetAvailability>[
    STATUS_PRESET_AVAILABILITY_UNSPECIFIED,
    STATUS_PRESET_AVAILABILITY_MEETING,
    STATUS_PRESET_AVAILABILITY_OUT_OF_OFFICE,
  ];

  static final $core.List<StatusPresetAvailability?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static StatusPresetAvailability? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const StatusPresetAvailability._(super.value, super.name);
}

class StatusPresetInputAvailability extends $pb.ProtobufEnum {
  static const StatusPresetInputAvailability
      STATUS_PRESET_INPUT_AVAILABILITY_UNSPECIFIED =
      StatusPresetInputAvailability._(0,
          _omitEnumNames ? '' : 'STATUS_PRESET_INPUT_AVAILABILITY_UNSPECIFIED');
  static const StatusPresetInputAvailability
      STATUS_PRESET_INPUT_AVAILABILITY_MEETING =
      StatusPresetInputAvailability._(
          1, _omitEnumNames ? '' : 'STATUS_PRESET_INPUT_AVAILABILITY_MEETING');
  static const StatusPresetInputAvailability
      STATUS_PRESET_INPUT_AVAILABILITY_OUT_OF_OFFICE =
      StatusPresetInputAvailability._(
          2,
          _omitEnumNames
              ? ''
              : 'STATUS_PRESET_INPUT_AVAILABILITY_OUT_OF_OFFICE');

  static const $core.List<StatusPresetInputAvailability> values =
      <StatusPresetInputAvailability>[
    STATUS_PRESET_INPUT_AVAILABILITY_UNSPECIFIED,
    STATUS_PRESET_INPUT_AVAILABILITY_MEETING,
    STATUS_PRESET_INPUT_AVAILABILITY_OUT_OF_OFFICE,
  ];

  static final $core.List<StatusPresetInputAvailability?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static StatusPresetInputAvailability? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const StatusPresetInputAvailability._(super.value, super.name);
}

class AccountRole extends $pb.ProtobufEnum {
  static const AccountRole ACCOUNT_ROLE_UNSPECIFIED =
      AccountRole._(0, _omitEnumNames ? '' : 'ACCOUNT_ROLE_UNSPECIFIED');
  static const AccountRole ACCOUNT_ROLE_OWNER =
      AccountRole._(1, _omitEnumNames ? '' : 'ACCOUNT_ROLE_OWNER');
  static const AccountRole ACCOUNT_ROLE_ADMIN =
      AccountRole._(2, _omitEnumNames ? '' : 'ACCOUNT_ROLE_ADMIN');
  static const AccountRole ACCOUNT_ROLE_MEMBER =
      AccountRole._(3, _omitEnumNames ? '' : 'ACCOUNT_ROLE_MEMBER');
  static const AccountRole ACCOUNT_ROLE_GUEST =
      AccountRole._(4, _omitEnumNames ? '' : 'ACCOUNT_ROLE_GUEST');
  static const AccountRole ACCOUNT_ROLE_BOT =
      AccountRole._(5, _omitEnumNames ? '' : 'ACCOUNT_ROLE_BOT');
  static const AccountRole ACCOUNT_ROLE_REMOTE =
      AccountRole._(6, _omitEnumNames ? '' : 'ACCOUNT_ROLE_REMOTE');

  static const $core.List<AccountRole> values = <AccountRole>[
    ACCOUNT_ROLE_UNSPECIFIED,
    ACCOUNT_ROLE_OWNER,
    ACCOUNT_ROLE_ADMIN,
    ACCOUNT_ROLE_MEMBER,
    ACCOUNT_ROLE_GUEST,
    ACCOUNT_ROLE_BOT,
    ACCOUNT_ROLE_REMOTE,
  ];

  static final $core.List<AccountRole?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static AccountRole? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AccountRole._(super.value, super.name);
}

class AccountPresence extends $pb.ProtobufEnum {
  static const AccountPresence ACCOUNT_PRESENCE_UNSPECIFIED = AccountPresence._(
      0, _omitEnumNames ? '' : 'ACCOUNT_PRESENCE_UNSPECIFIED');
  static const AccountPresence ACCOUNT_PRESENCE_ONLINE =
      AccountPresence._(1, _omitEnumNames ? '' : 'ACCOUNT_PRESENCE_ONLINE');
  static const AccountPresence ACCOUNT_PRESENCE_AWAY =
      AccountPresence._(2, _omitEnumNames ? '' : 'ACCOUNT_PRESENCE_AWAY');
  static const AccountPresence ACCOUNT_PRESENCE_OFFLINE =
      AccountPresence._(3, _omitEnumNames ? '' : 'ACCOUNT_PRESENCE_OFFLINE');

  static const $core.List<AccountPresence> values = <AccountPresence>[
    ACCOUNT_PRESENCE_UNSPECIFIED,
    ACCOUNT_PRESENCE_ONLINE,
    ACCOUNT_PRESENCE_AWAY,
    ACCOUNT_PRESENCE_OFFLINE,
  ];

  static final $core.List<AccountPresence?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static AccountPresence? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AccountPresence._(super.value, super.name);
}

class AccountLastSeen extends $pb.ProtobufEnum {
  static const AccountLastSeen ACCOUNT_LAST_SEEN_UNSPECIFIED =
      AccountLastSeen._(
          0, _omitEnumNames ? '' : 'ACCOUNT_LAST_SEEN_UNSPECIFIED');
  static const AccountLastSeen ACCOUNT_LAST_SEEN_ONLINE =
      AccountLastSeen._(1, _omitEnumNames ? '' : 'ACCOUNT_LAST_SEEN_ONLINE');
  static const AccountLastSeen ACCOUNT_LAST_SEEN_RECENTLY =
      AccountLastSeen._(2, _omitEnumNames ? '' : 'ACCOUNT_LAST_SEEN_RECENTLY');
  static const AccountLastSeen ACCOUNT_LAST_SEEN_WITHIN_WEEK =
      AccountLastSeen._(
          3, _omitEnumNames ? '' : 'ACCOUNT_LAST_SEEN_WITHIN_WEEK');
  static const AccountLastSeen ACCOUNT_LAST_SEEN_LONG_AGO =
      AccountLastSeen._(4, _omitEnumNames ? '' : 'ACCOUNT_LAST_SEEN_LONG_AGO');

  static const $core.List<AccountLastSeen> values = <AccountLastSeen>[
    ACCOUNT_LAST_SEEN_UNSPECIFIED,
    ACCOUNT_LAST_SEEN_ONLINE,
    ACCOUNT_LAST_SEEN_RECENTLY,
    ACCOUNT_LAST_SEEN_WITHIN_WEEK,
    ACCOUNT_LAST_SEEN_LONG_AGO,
  ];

  static final $core.List<AccountLastSeen?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static AccountLastSeen? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AccountLastSeen._(super.value, super.name);
}

class GroupColor extends $pb.ProtobufEnum {
  static const GroupColor GROUP_COLOR_UNSPECIFIED =
      GroupColor._(0, _omitEnumNames ? '' : 'GROUP_COLOR_UNSPECIFIED');
  static const GroupColor GROUP_COLOR_LAVENDER =
      GroupColor._(1, _omitEnumNames ? '' : 'GROUP_COLOR_LAVENDER');
  static const GroupColor GROUP_COLOR_CYAN =
      GroupColor._(2, _omitEnumNames ? '' : 'GROUP_COLOR_CYAN');
  static const GroupColor GROUP_COLOR_PINK =
      GroupColor._(3, _omitEnumNames ? '' : 'GROUP_COLOR_PINK');
  static const GroupColor GROUP_COLOR_YELLOW =
      GroupColor._(4, _omitEnumNames ? '' : 'GROUP_COLOR_YELLOW');
  static const GroupColor GROUP_COLOR_GREEN =
      GroupColor._(5, _omitEnumNames ? '' : 'GROUP_COLOR_GREEN');
  static const GroupColor GROUP_COLOR_RED =
      GroupColor._(6, _omitEnumNames ? '' : 'GROUP_COLOR_RED');
  static const GroupColor GROUP_COLOR_GREY =
      GroupColor._(7, _omitEnumNames ? '' : 'GROUP_COLOR_GREY');

  static const $core.List<GroupColor> values = <GroupColor>[
    GROUP_COLOR_UNSPECIFIED,
    GROUP_COLOR_LAVENDER,
    GROUP_COLOR_CYAN,
    GROUP_COLOR_PINK,
    GROUP_COLOR_YELLOW,
    GROUP_COLOR_GREEN,
    GROUP_COLOR_RED,
    GROUP_COLOR_GREY,
  ];

  static final $core.List<GroupColor?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static GroupColor? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const GroupColor._(super.value, super.name);
}

class GroupBuiltin extends $pb.ProtobufEnum {
  static const GroupBuiltin GROUP_BUILTIN_UNSPECIFIED =
      GroupBuiltin._(0, _omitEnumNames ? '' : 'GROUP_BUILTIN_UNSPECIFIED');
  static const GroupBuiltin GROUP_BUILTIN_EVERYONE =
      GroupBuiltin._(1, _omitEnumNames ? '' : 'GROUP_BUILTIN_EVERYONE');
  static const GroupBuiltin GROUP_BUILTIN_ADMINS =
      GroupBuiltin._(2, _omitEnumNames ? '' : 'GROUP_BUILTIN_ADMINS');

  static const $core.List<GroupBuiltin> values = <GroupBuiltin>[
    GROUP_BUILTIN_UNSPECIFIED,
    GROUP_BUILTIN_EVERYONE,
    GROUP_BUILTIN_ADMINS,
  ];

  static final $core.List<GroupBuiltin?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static GroupBuiltin? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const GroupBuiltin._(super.value, super.name);
}

class ChatRequestKind extends $pb.ProtobufEnum {
  static const ChatRequestKind CHAT_REQUEST_KIND_UNSPECIFIED =
      ChatRequestKind._(
          0, _omitEnumNames ? '' : 'CHAT_REQUEST_KIND_UNSPECIFIED');
  static const ChatRequestKind CHAT_REQUEST_KIND_DIRECT =
      ChatRequestKind._(1, _omitEnumNames ? '' : 'CHAT_REQUEST_KIND_DIRECT');
  static const ChatRequestKind CHAT_REQUEST_KIND_SEALED =
      ChatRequestKind._(2, _omitEnumNames ? '' : 'CHAT_REQUEST_KIND_SEALED');

  static const $core.List<ChatRequestKind> values = <ChatRequestKind>[
    CHAT_REQUEST_KIND_UNSPECIFIED,
    CHAT_REQUEST_KIND_DIRECT,
    CHAT_REQUEST_KIND_SEALED,
  ];

  static final $core.List<ChatRequestKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ChatRequestKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ChatRequestKind._(super.value, super.name);
}

class ConversationKind extends $pb.ProtobufEnum {
  static const ConversationKind CONVERSATION_KIND_UNSPECIFIED =
      ConversationKind._(
          0, _omitEnumNames ? '' : 'CONVERSATION_KIND_UNSPECIFIED');
  static const ConversationKind CONVERSATION_KIND_CHANNEL =
      ConversationKind._(1, _omitEnumNames ? '' : 'CONVERSATION_KIND_CHANNEL');
  static const ConversationKind CONVERSATION_KIND_SEALED =
      ConversationKind._(2, _omitEnumNames ? '' : 'CONVERSATION_KIND_SEALED');

  static const $core.List<ConversationKind> values = <ConversationKind>[
    CONVERSATION_KIND_UNSPECIFIED,
    CONVERSATION_KIND_CHANNEL,
    CONVERSATION_KIND_SEALED,
  ];

  static final $core.List<ConversationKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ConversationKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ConversationKind._(super.value, super.name);
}

class ConversationAccess extends $pb.ProtobufEnum {
  static const ConversationAccess CONVERSATION_ACCESS_UNSPECIFIED =
      ConversationAccess._(
          0, _omitEnumNames ? '' : 'CONVERSATION_ACCESS_UNSPECIFIED');
  static const ConversationAccess CONVERSATION_ACCESS_PUBLIC =
      ConversationAccess._(
          1, _omitEnumNames ? '' : 'CONVERSATION_ACCESS_PUBLIC');
  static const ConversationAccess CONVERSATION_ACCESS_KNOCK =
      ConversationAccess._(
          2, _omitEnumNames ? '' : 'CONVERSATION_ACCESS_KNOCK');
  static const ConversationAccess CONVERSATION_ACCESS_PRIVATE =
      ConversationAccess._(
          3, _omitEnumNames ? '' : 'CONVERSATION_ACCESS_PRIVATE');

  static const $core.List<ConversationAccess> values = <ConversationAccess>[
    CONVERSATION_ACCESS_UNSPECIFIED,
    CONVERSATION_ACCESS_PUBLIC,
    CONVERSATION_ACCESS_KNOCK,
    CONVERSATION_ACCESS_PRIVATE,
  ];

  static final $core.List<ConversationAccess?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ConversationAccess? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ConversationAccess._(super.value, super.name);
}

class ConversationDefaultLevel extends $pb.ProtobufEnum {
  static const ConversationDefaultLevel CONVERSATION_DEFAULT_LEVEL_UNSPECIFIED =
      ConversationDefaultLevel._(
          0, _omitEnumNames ? '' : 'CONVERSATION_DEFAULT_LEVEL_UNSPECIFIED');
  static const ConversationDefaultLevel CONVERSATION_DEFAULT_LEVEL_READER =
      ConversationDefaultLevel._(
          1, _omitEnumNames ? '' : 'CONVERSATION_DEFAULT_LEVEL_READER');
  static const ConversationDefaultLevel CONVERSATION_DEFAULT_LEVEL_MEMBER =
      ConversationDefaultLevel._(
          2, _omitEnumNames ? '' : 'CONVERSATION_DEFAULT_LEVEL_MEMBER');

  static const $core.List<ConversationDefaultLevel> values =
      <ConversationDefaultLevel>[
    CONVERSATION_DEFAULT_LEVEL_UNSPECIFIED,
    CONVERSATION_DEFAULT_LEVEL_READER,
    CONVERSATION_DEFAULT_LEVEL_MEMBER,
  ];

  static final $core.List<ConversationDefaultLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ConversationDefaultLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ConversationDefaultLevel._(super.value, super.name);
}

class ConversationBroadMentions extends $pb.ProtobufEnum {
  static const ConversationBroadMentions
      CONVERSATION_BROAD_MENTIONS_UNSPECIFIED = ConversationBroadMentions._(
          0, _omitEnumNames ? '' : 'CONVERSATION_BROAD_MENTIONS_UNSPECIFIED');
  static const ConversationBroadMentions CONVERSATION_BROAD_MENTIONS_SERVER =
      ConversationBroadMentions._(
          1, _omitEnumNames ? '' : 'CONVERSATION_BROAD_MENTIONS_SERVER');
  static const ConversationBroadMentions CONVERSATION_BROAD_MENTIONS_ANYONE =
      ConversationBroadMentions._(
          2, _omitEnumNames ? '' : 'CONVERSATION_BROAD_MENTIONS_ANYONE');
  static const ConversationBroadMentions CONVERSATION_BROAD_MENTIONS_MANAGERS =
      ConversationBroadMentions._(
          3, _omitEnumNames ? '' : 'CONVERSATION_BROAD_MENTIONS_MANAGERS');

  static const $core.List<ConversationBroadMentions> values =
      <ConversationBroadMentions>[
    CONVERSATION_BROAD_MENTIONS_UNSPECIFIED,
    CONVERSATION_BROAD_MENTIONS_SERVER,
    CONVERSATION_BROAD_MENTIONS_ANYONE,
    CONVERSATION_BROAD_MENTIONS_MANAGERS,
  ];

  static final $core.List<ConversationBroadMentions?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ConversationBroadMentions? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ConversationBroadMentions._(super.value, super.name);
}

class ConversationLazyDelivery extends $pb.ProtobufEnum {
  static const ConversationLazyDelivery CONVERSATION_LAZY_DELIVERY_UNSPECIFIED =
      ConversationLazyDelivery._(
          0, _omitEnumNames ? '' : 'CONVERSATION_LAZY_DELIVERY_UNSPECIFIED');
  static const ConversationLazyDelivery CONVERSATION_LAZY_DELIVERY_SERVER =
      ConversationLazyDelivery._(
          1, _omitEnumNames ? '' : 'CONVERSATION_LAZY_DELIVERY_SERVER');
  static const ConversationLazyDelivery CONVERSATION_LAZY_DELIVERY_ON =
      ConversationLazyDelivery._(
          2, _omitEnumNames ? '' : 'CONVERSATION_LAZY_DELIVERY_ON');
  static const ConversationLazyDelivery CONVERSATION_LAZY_DELIVERY_OFF =
      ConversationLazyDelivery._(
          3, _omitEnumNames ? '' : 'CONVERSATION_LAZY_DELIVERY_OFF');

  static const $core.List<ConversationLazyDelivery> values =
      <ConversationLazyDelivery>[
    CONVERSATION_LAZY_DELIVERY_UNSPECIFIED,
    CONVERSATION_LAZY_DELIVERY_SERVER,
    CONVERSATION_LAZY_DELIVERY_ON,
    CONVERSATION_LAZY_DELIVERY_OFF,
  ];

  static final $core.List<ConversationLazyDelivery?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ConversationLazyDelivery? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ConversationLazyDelivery._(super.value, super.name);
}

class ConversationChatState extends $pb.ProtobufEnum {
  static const ConversationChatState CONVERSATION_CHAT_STATE_UNSPECIFIED =
      ConversationChatState._(
          0, _omitEnumNames ? '' : 'CONVERSATION_CHAT_STATE_UNSPECIFIED');
  static const ConversationChatState CONVERSATION_CHAT_STATE_WAITING =
      ConversationChatState._(
          1, _omitEnumNames ? '' : 'CONVERSATION_CHAT_STATE_WAITING');
  static const ConversationChatState CONVERSATION_CHAT_STATE_DECLINED =
      ConversationChatState._(
          2, _omitEnumNames ? '' : 'CONVERSATION_CHAT_STATE_DECLINED');
  static const ConversationChatState CONVERSATION_CHAT_STATE_CLOSED =
      ConversationChatState._(
          3, _omitEnumNames ? '' : 'CONVERSATION_CHAT_STATE_CLOSED');

  static const $core.List<ConversationChatState> values =
      <ConversationChatState>[
    CONVERSATION_CHAT_STATE_UNSPECIFIED,
    CONVERSATION_CHAT_STATE_WAITING,
    CONVERSATION_CHAT_STATE_DECLINED,
    CONVERSATION_CHAT_STATE_CLOSED,
  ];

  static final $core.List<ConversationChatState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ConversationChatState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ConversationChatState._(super.value, super.name);
}

class CallTrackSource extends $pb.ProtobufEnum {
  static const CallTrackSource CALL_TRACK_SOURCE_UNSPECIFIED =
      CallTrackSource._(
          0, _omitEnumNames ? '' : 'CALL_TRACK_SOURCE_UNSPECIFIED');
  static const CallTrackSource CALL_TRACK_SOURCE_MICROPHONE = CallTrackSource._(
      1, _omitEnumNames ? '' : 'CALL_TRACK_SOURCE_MICROPHONE');
  static const CallTrackSource CALL_TRACK_SOURCE_CAMERA =
      CallTrackSource._(2, _omitEnumNames ? '' : 'CALL_TRACK_SOURCE_CAMERA');
  static const CallTrackSource CALL_TRACK_SOURCE_SCREEN =
      CallTrackSource._(3, _omitEnumNames ? '' : 'CALL_TRACK_SOURCE_SCREEN');

  static const $core.List<CallTrackSource> values = <CallTrackSource>[
    CALL_TRACK_SOURCE_UNSPECIFIED,
    CALL_TRACK_SOURCE_MICROPHONE,
    CALL_TRACK_SOURCE_CAMERA,
    CALL_TRACK_SOURCE_SCREEN,
  ];

  static final $core.List<CallTrackSource?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CallTrackSource? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallTrackSource._(super.value, super.name);
}

class CallTrackKind extends $pb.ProtobufEnum {
  static const CallTrackKind CALL_TRACK_KIND_UNSPECIFIED =
      CallTrackKind._(0, _omitEnumNames ? '' : 'CALL_TRACK_KIND_UNSPECIFIED');
  static const CallTrackKind CALL_TRACK_KIND_AUDIO =
      CallTrackKind._(1, _omitEnumNames ? '' : 'CALL_TRACK_KIND_AUDIO');
  static const CallTrackKind CALL_TRACK_KIND_VIDEO =
      CallTrackKind._(2, _omitEnumNames ? '' : 'CALL_TRACK_KIND_VIDEO');

  static const $core.List<CallTrackKind> values = <CallTrackKind>[
    CALL_TRACK_KIND_UNSPECIFIED,
    CALL_TRACK_KIND_AUDIO,
    CALL_TRACK_KIND_VIDEO,
  ];

  static final $core.List<CallTrackKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static CallTrackKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallTrackKind._(super.value, super.name);
}

class MembershipLevel extends $pb.ProtobufEnum {
  static const MembershipLevel MEMBERSHIP_LEVEL_UNSPECIFIED = MembershipLevel._(
      0, _omitEnumNames ? '' : 'MEMBERSHIP_LEVEL_UNSPECIFIED');
  static const MembershipLevel MEMBERSHIP_LEVEL_READER =
      MembershipLevel._(1, _omitEnumNames ? '' : 'MEMBERSHIP_LEVEL_READER');
  static const MembershipLevel MEMBERSHIP_LEVEL_MEMBER =
      MembershipLevel._(2, _omitEnumNames ? '' : 'MEMBERSHIP_LEVEL_MEMBER');
  static const MembershipLevel MEMBERSHIP_LEVEL_MANAGER =
      MembershipLevel._(3, _omitEnumNames ? '' : 'MEMBERSHIP_LEVEL_MANAGER');

  static const $core.List<MembershipLevel> values = <MembershipLevel>[
    MEMBERSHIP_LEVEL_UNSPECIFIED,
    MEMBERSHIP_LEVEL_READER,
    MEMBERSHIP_LEVEL_MEMBER,
    MEMBERSHIP_LEVEL_MANAGER,
  ];

  static final $core.List<MembershipLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static MembershipLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MembershipLevel._(super.value, super.name);
}

class MembershipMode extends $pb.ProtobufEnum {
  static const MembershipMode MEMBERSHIP_MODE_UNSPECIFIED =
      MembershipMode._(0, _omitEnumNames ? '' : 'MEMBERSHIP_MODE_UNSPECIFIED');
  static const MembershipMode MEMBERSHIP_MODE_ANY =
      MembershipMode._(1, _omitEnumNames ? '' : 'MEMBERSHIP_MODE_ANY');
  static const MembershipMode MEMBERSHIP_MODE_MENTIONS =
      MembershipMode._(2, _omitEnumNames ? '' : 'MEMBERSHIP_MODE_MENTIONS');
  static const MembershipMode MEMBERSHIP_MODE_NOTHING =
      MembershipMode._(3, _omitEnumNames ? '' : 'MEMBERSHIP_MODE_NOTHING');

  static const $core.List<MembershipMode> values = <MembershipMode>[
    MEMBERSHIP_MODE_UNSPECIFIED,
    MEMBERSHIP_MODE_ANY,
    MEMBERSHIP_MODE_MENTIONS,
    MEMBERSHIP_MODE_NOTHING,
  ];

  static final $core.List<MembershipMode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static MembershipMode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MembershipMode._(super.value, super.name);
}

class MembershipShow extends $pb.ProtobufEnum {
  static const MembershipShow MEMBERSHIP_SHOW_UNSPECIFIED =
      MembershipShow._(0, _omitEnumNames ? '' : 'MEMBERSHIP_SHOW_UNSPECIFIED');
  static const MembershipShow MEMBERSHIP_SHOW_ALWAYS =
      MembershipShow._(1, _omitEnumNames ? '' : 'MEMBERSHIP_SHOW_ALWAYS');
  static const MembershipShow MEMBERSHIP_SHOW_ACTIVITY =
      MembershipShow._(2, _omitEnumNames ? '' : 'MEMBERSHIP_SHOW_ACTIVITY');
  static const MembershipShow MEMBERSHIP_SHOW_NOTIFICATION =
      MembershipShow._(3, _omitEnumNames ? '' : 'MEMBERSHIP_SHOW_NOTIFICATION');
  static const MembershipShow MEMBERSHIP_SHOW_HIDDEN =
      MembershipShow._(4, _omitEnumNames ? '' : 'MEMBERSHIP_SHOW_HIDDEN');

  static const $core.List<MembershipShow> values = <MembershipShow>[
    MEMBERSHIP_SHOW_UNSPECIFIED,
    MEMBERSHIP_SHOW_ALWAYS,
    MEMBERSHIP_SHOW_ACTIVITY,
    MEMBERSHIP_SHOW_NOTIFICATION,
    MEMBERSHIP_SHOW_HIDDEN,
  ];

  static final $core.List<MembershipShow?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static MembershipShow? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MembershipShow._(super.value, super.name);
}

class MembershipThreadView extends $pb.ProtobufEnum {
  static const MembershipThreadView MEMBERSHIP_THREAD_VIEW_UNSPECIFIED =
      MembershipThreadView._(
          0, _omitEnumNames ? '' : 'MEMBERSHIP_THREAD_VIEW_UNSPECIFIED');
  static const MembershipThreadView MEMBERSHIP_THREAD_VIEW_THREADED =
      MembershipThreadView._(
          1, _omitEnumNames ? '' : 'MEMBERSHIP_THREAD_VIEW_THREADED');
  static const MembershipThreadView MEMBERSHIP_THREAD_VIEW_FLAT =
      MembershipThreadView._(
          2, _omitEnumNames ? '' : 'MEMBERSHIP_THREAD_VIEW_FLAT');

  static const $core.List<MembershipThreadView> values = <MembershipThreadView>[
    MEMBERSHIP_THREAD_VIEW_UNSPECIFIED,
    MEMBERSHIP_THREAD_VIEW_THREADED,
    MEMBERSHIP_THREAD_VIEW_FLAT,
  ];

  static final $core.List<MembershipThreadView?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static MembershipThreadView? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MembershipThreadView._(super.value, super.name);
}

class ScheduledItemKind extends $pb.ProtobufEnum {
  static const ScheduledItemKind SCHEDULED_ITEM_KIND_UNSPECIFIED =
      ScheduledItemKind._(
          0, _omitEnumNames ? '' : 'SCHEDULED_ITEM_KIND_UNSPECIFIED');
  static const ScheduledItemKind SCHEDULED_ITEM_KIND_MESSAGE =
      ScheduledItemKind._(
          1, _omitEnumNames ? '' : 'SCHEDULED_ITEM_KIND_MESSAGE');
  static const ScheduledItemKind SCHEDULED_ITEM_KIND_REMINDER =
      ScheduledItemKind._(
          2, _omitEnumNames ? '' : 'SCHEDULED_ITEM_KIND_REMINDER');
  static const ScheduledItemKind SCHEDULED_ITEM_KIND_FORM =
      ScheduledItemKind._(3, _omitEnumNames ? '' : 'SCHEDULED_ITEM_KIND_FORM');
  static const ScheduledItemKind SCHEDULED_ITEM_KIND_WATCH =
      ScheduledItemKind._(4, _omitEnumNames ? '' : 'SCHEDULED_ITEM_KIND_WATCH');

  static const $core.List<ScheduledItemKind> values = <ScheduledItemKind>[
    SCHEDULED_ITEM_KIND_UNSPECIFIED,
    SCHEDULED_ITEM_KIND_MESSAGE,
    SCHEDULED_ITEM_KIND_REMINDER,
    SCHEDULED_ITEM_KIND_FORM,
    SCHEDULED_ITEM_KIND_WATCH,
  ];

  static final $core.List<ScheduledItemKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static ScheduledItemKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ScheduledItemKind._(super.value, super.name);
}

class FormInputPreset extends $pb.ProtobufEnum {
  static const FormInputPreset FORM_INPUT_PRESET_UNSPECIFIED =
      FormInputPreset._(
          0, _omitEnumNames ? '' : 'FORM_INPUT_PRESET_UNSPECIFIED');
  static const FormInputPreset FORM_INPUT_PRESET_POLL =
      FormInputPreset._(1, _omitEnumNames ? '' : 'FORM_INPUT_PRESET_POLL');
  static const FormInputPreset FORM_INPUT_PRESET_RSVP =
      FormInputPreset._(2, _omitEnumNames ? '' : 'FORM_INPUT_PRESET_RSVP');
  static const FormInputPreset FORM_INPUT_PRESET_SIGNUP =
      FormInputPreset._(3, _omitEnumNames ? '' : 'FORM_INPUT_PRESET_SIGNUP');
  static const FormInputPreset FORM_INPUT_PRESET_APPROVE =
      FormInputPreset._(4, _omitEnumNames ? '' : 'FORM_INPUT_PRESET_APPROVE');
  static const FormInputPreset FORM_INPUT_PRESET_CHECKIN =
      FormInputPreset._(5, _omitEnumNames ? '' : 'FORM_INPUT_PRESET_CHECKIN');
  static const FormInputPreset FORM_INPUT_PRESET_TICKET =
      FormInputPreset._(6, _omitEnumNames ? '' : 'FORM_INPUT_PRESET_TICKET');
  static const FormInputPreset FORM_INPUT_PRESET_ROLES =
      FormInputPreset._(7, _omitEnumNames ? '' : 'FORM_INPUT_PRESET_ROLES');

  static const $core.List<FormInputPreset> values = <FormInputPreset>[
    FORM_INPUT_PRESET_UNSPECIFIED,
    FORM_INPUT_PRESET_POLL,
    FORM_INPUT_PRESET_RSVP,
    FORM_INPUT_PRESET_SIGNUP,
    FORM_INPUT_PRESET_APPROVE,
    FORM_INPUT_PRESET_CHECKIN,
    FORM_INPUT_PRESET_TICKET,
    FORM_INPUT_PRESET_ROLES,
  ];

  static final $core.List<FormInputPreset?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static FormInputPreset? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FormInputPreset._(super.value, super.name);
}

class FormInputResults extends $pb.ProtobufEnum {
  static const FormInputResults FORM_INPUT_RESULTS_UNSPECIFIED =
      FormInputResults._(
          0, _omitEnumNames ? '' : 'FORM_INPUT_RESULTS_UNSPECIFIED');
  static const FormInputResults FORM_INPUT_RESULTS_LIVE =
      FormInputResults._(1, _omitEnumNames ? '' : 'FORM_INPUT_RESULTS_LIVE');
  static const FormInputResults FORM_INPUT_RESULTS_AFTER_ANSWER =
      FormInputResults._(
          2, _omitEnumNames ? '' : 'FORM_INPUT_RESULTS_AFTER_ANSWER');
  static const FormInputResults FORM_INPUT_RESULTS_AFTER_CLOSE =
      FormInputResults._(
          3, _omitEnumNames ? '' : 'FORM_INPUT_RESULTS_AFTER_CLOSE');

  static const $core.List<FormInputResults> values = <FormInputResults>[
    FORM_INPUT_RESULTS_UNSPECIFIED,
    FORM_INPUT_RESULTS_LIVE,
    FORM_INPUT_RESULTS_AFTER_ANSWER,
    FORM_INPUT_RESULTS_AFTER_CLOSE,
  ];

  static final $core.List<FormInputResults?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static FormInputResults? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FormInputResults._(super.value, super.name);
}

class ChanceInputKind extends $pb.ProtobufEnum {
  static const ChanceInputKind CHANCE_INPUT_KIND_UNSPECIFIED =
      ChanceInputKind._(
          0, _omitEnumNames ? '' : 'CHANCE_INPUT_KIND_UNSPECIFIED');
  static const ChanceInputKind CHANCE_INPUT_KIND_ROLL =
      ChanceInputKind._(1, _omitEnumNames ? '' : 'CHANCE_INPUT_KIND_ROLL');
  static const ChanceInputKind CHANCE_INPUT_KIND_FLIP =
      ChanceInputKind._(2, _omitEnumNames ? '' : 'CHANCE_INPUT_KIND_FLIP');
  static const ChanceInputKind CHANCE_INPUT_KIND_PICK =
      ChanceInputKind._(3, _omitEnumNames ? '' : 'CHANCE_INPUT_KIND_PICK');
  static const ChanceInputKind CHANCE_INPUT_KIND_SHUFFLE =
      ChanceInputKind._(4, _omitEnumNames ? '' : 'CHANCE_INPUT_KIND_SHUFFLE');
  static const ChanceInputKind CHANCE_INPUT_KIND_DRAW =
      ChanceInputKind._(5, _omitEnumNames ? '' : 'CHANCE_INPUT_KIND_DRAW');
  static const ChanceInputKind CHANCE_INPUT_KIND_TAROT =
      ChanceInputKind._(6, _omitEnumNames ? '' : 'CHANCE_INPUT_KIND_TAROT');

  static const $core.List<ChanceInputKind> values = <ChanceInputKind>[
    CHANCE_INPUT_KIND_UNSPECIFIED,
    CHANCE_INPUT_KIND_ROLL,
    CHANCE_INPUT_KIND_FLIP,
    CHANCE_INPUT_KIND_PICK,
    CHANCE_INPUT_KIND_SHUFFLE,
    CHANCE_INPUT_KIND_DRAW,
    CHANCE_INPUT_KIND_TAROT,
  ];

  static final $core.List<ChanceInputKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static ChanceInputKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ChanceInputKind._(super.value, super.name);
}

class InstantHintPrecision extends $pb.ProtobufEnum {
  static const InstantHintPrecision INSTANT_HINT_PRECISION_UNSPECIFIED =
      InstantHintPrecision._(
          0, _omitEnumNames ? '' : 'INSTANT_HINT_PRECISION_UNSPECIFIED');
  static const InstantHintPrecision INSTANT_HINT_PRECISION_MINUTE =
      InstantHintPrecision._(
          1, _omitEnumNames ? '' : 'INSTANT_HINT_PRECISION_MINUTE');
  static const InstantHintPrecision INSTANT_HINT_PRECISION_SECOND =
      InstantHintPrecision._(
          2, _omitEnumNames ? '' : 'INSTANT_HINT_PRECISION_SECOND');

  static const $core.List<InstantHintPrecision> values = <InstantHintPrecision>[
    INSTANT_HINT_PRECISION_UNSPECIFIED,
    INSTANT_HINT_PRECISION_MINUTE,
    INSTANT_HINT_PRECISION_SECOND,
  ];

  static final $core.List<InstantHintPrecision?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static InstantHintPrecision? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const InstantHintPrecision._(super.value, super.name);
}

class ObjectHintKind extends $pb.ProtobufEnum {
  static const ObjectHintKind OBJECT_HINT_KIND_UNSPECIFIED =
      ObjectHintKind._(0, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_UNSPECIFIED');
  static const ObjectHintKind OBJECT_HINT_KIND_PLAIN =
      ObjectHintKind._(1, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_PLAIN');
  static const ObjectHintKind OBJECT_HINT_KIND_REFERENCE =
      ObjectHintKind._(2, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_REFERENCE');
  static const ObjectHintKind OBJECT_HINT_KIND_MESSAGE_LINK = ObjectHintKind._(
      3, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_MESSAGE_LINK');
  static const ObjectHintKind OBJECT_HINT_KIND_MEASURE =
      ObjectHintKind._(4, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_MEASURE');
  static const ObjectHintKind OBJECT_HINT_KIND_COLOUR =
      ObjectHintKind._(5, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_COLOUR');
  static const ObjectHintKind OBJECT_HINT_KIND_PHONE =
      ObjectHintKind._(6, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_PHONE');
  static const ObjectHintKind OBJECT_HINT_KIND_EMAIL =
      ObjectHintKind._(7, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_EMAIL');
  static const ObjectHintKind OBJECT_HINT_KIND_GEO =
      ObjectHintKind._(8, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_GEO');
  static const ObjectHintKind OBJECT_HINT_KIND_DICE =
      ObjectHintKind._(9, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_DICE');
  static const ObjectHintKind OBJECT_HINT_KIND_NUMBER =
      ObjectHintKind._(10, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_NUMBER');
  static const ObjectHintKind OBJECT_HINT_KIND_CODEPOINT =
      ObjectHintKind._(11, _omitEnumNames ? '' : 'OBJECT_HINT_KIND_CODEPOINT');

  static const $core.List<ObjectHintKind> values = <ObjectHintKind>[
    OBJECT_HINT_KIND_UNSPECIFIED,
    OBJECT_HINT_KIND_PLAIN,
    OBJECT_HINT_KIND_REFERENCE,
    OBJECT_HINT_KIND_MESSAGE_LINK,
    OBJECT_HINT_KIND_MEASURE,
    OBJECT_HINT_KIND_COLOUR,
    OBJECT_HINT_KIND_PHONE,
    OBJECT_HINT_KIND_EMAIL,
    OBJECT_HINT_KIND_GEO,
    OBJECT_HINT_KIND_DICE,
    OBJECT_HINT_KIND_NUMBER,
    OBJECT_HINT_KIND_CODEPOINT,
  ];

  static final $core.List<ObjectHintKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 11);
  static ObjectHintKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ObjectHintKind._(super.value, super.name);
}

class RepeatEvery extends $pb.ProtobufEnum {
  static const RepeatEvery REPEAT_EVERY_UNSPECIFIED =
      RepeatEvery._(0, _omitEnumNames ? '' : 'REPEAT_EVERY_UNSPECIFIED');
  static const RepeatEvery REPEAT_EVERY_DAY =
      RepeatEvery._(1, _omitEnumNames ? '' : 'REPEAT_EVERY_DAY');
  static const RepeatEvery REPEAT_EVERY_WEEK =
      RepeatEvery._(2, _omitEnumNames ? '' : 'REPEAT_EVERY_WEEK');
  static const RepeatEvery REPEAT_EVERY_MONTH =
      RepeatEvery._(3, _omitEnumNames ? '' : 'REPEAT_EVERY_MONTH');

  static const $core.List<RepeatEvery> values = <RepeatEvery>[
    REPEAT_EVERY_UNSPECIFIED,
    REPEAT_EVERY_DAY,
    REPEAT_EVERY_WEEK,
    REPEAT_EVERY_MONTH,
  ];

  static final $core.List<RepeatEvery?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static RepeatEvery? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const RepeatEvery._(super.value, super.name);
}

class UpdateConversationRequestAccess extends $pb.ProtobufEnum {
  static const UpdateConversationRequestAccess
      UPDATE_CONVERSATION_REQUEST_ACCESS_UNSPECIFIED =
      UpdateConversationRequestAccess._(
          0,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_ACCESS_UNSPECIFIED');
  static const UpdateConversationRequestAccess
      UPDATE_CONVERSATION_REQUEST_ACCESS_PUBLIC =
      UpdateConversationRequestAccess._(
          1, _omitEnumNames ? '' : 'UPDATE_CONVERSATION_REQUEST_ACCESS_PUBLIC');
  static const UpdateConversationRequestAccess
      UPDATE_CONVERSATION_REQUEST_ACCESS_KNOCK =
      UpdateConversationRequestAccess._(
          2, _omitEnumNames ? '' : 'UPDATE_CONVERSATION_REQUEST_ACCESS_KNOCK');
  static const UpdateConversationRequestAccess
      UPDATE_CONVERSATION_REQUEST_ACCESS_PRIVATE =
      UpdateConversationRequestAccess._(3,
          _omitEnumNames ? '' : 'UPDATE_CONVERSATION_REQUEST_ACCESS_PRIVATE');

  static const $core.List<UpdateConversationRequestAccess> values =
      <UpdateConversationRequestAccess>[
    UPDATE_CONVERSATION_REQUEST_ACCESS_UNSPECIFIED,
    UPDATE_CONVERSATION_REQUEST_ACCESS_PUBLIC,
    UPDATE_CONVERSATION_REQUEST_ACCESS_KNOCK,
    UPDATE_CONVERSATION_REQUEST_ACCESS_PRIVATE,
  ];

  static final $core.List<UpdateConversationRequestAccess?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static UpdateConversationRequestAccess? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdateConversationRequestAccess._(super.value, super.name);
}

class UpdateConversationRequestDefaultLevel extends $pb.ProtobufEnum {
  static const UpdateConversationRequestDefaultLevel
      UPDATE_CONVERSATION_REQUEST_DEFAULT_LEVEL_UNSPECIFIED =
      UpdateConversationRequestDefaultLevel._(
          0,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_DEFAULT_LEVEL_UNSPECIFIED');
  static const UpdateConversationRequestDefaultLevel
      UPDATE_CONVERSATION_REQUEST_DEFAULT_LEVEL_READER =
      UpdateConversationRequestDefaultLevel._(
          1,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_DEFAULT_LEVEL_READER');
  static const UpdateConversationRequestDefaultLevel
      UPDATE_CONVERSATION_REQUEST_DEFAULT_LEVEL_MEMBER =
      UpdateConversationRequestDefaultLevel._(
          2,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_DEFAULT_LEVEL_MEMBER');

  static const $core.List<UpdateConversationRequestDefaultLevel> values =
      <UpdateConversationRequestDefaultLevel>[
    UPDATE_CONVERSATION_REQUEST_DEFAULT_LEVEL_UNSPECIFIED,
    UPDATE_CONVERSATION_REQUEST_DEFAULT_LEVEL_READER,
    UPDATE_CONVERSATION_REQUEST_DEFAULT_LEVEL_MEMBER,
  ];

  static final $core.List<UpdateConversationRequestDefaultLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static UpdateConversationRequestDefaultLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdateConversationRequestDefaultLevel._(super.value, super.name);
}

class UpdateConversationRequestBroadMentions extends $pb.ProtobufEnum {
  static const UpdateConversationRequestBroadMentions
      UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_UNSPECIFIED =
      UpdateConversationRequestBroadMentions._(
          0,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_UNSPECIFIED');
  static const UpdateConversationRequestBroadMentions
      UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_SERVER =
      UpdateConversationRequestBroadMentions._(
          1,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_SERVER');
  static const UpdateConversationRequestBroadMentions
      UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_ANYONE =
      UpdateConversationRequestBroadMentions._(
          2,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_ANYONE');
  static const UpdateConversationRequestBroadMentions
      UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_MANAGERS =
      UpdateConversationRequestBroadMentions._(
          3,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_MANAGERS');

  static const $core.List<UpdateConversationRequestBroadMentions> values =
      <UpdateConversationRequestBroadMentions>[
    UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_UNSPECIFIED,
    UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_SERVER,
    UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_ANYONE,
    UPDATE_CONVERSATION_REQUEST_BROAD_MENTIONS_MANAGERS,
  ];

  static final $core.List<UpdateConversationRequestBroadMentions?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static UpdateConversationRequestBroadMentions? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdateConversationRequestBroadMentions._(super.value, super.name);
}

class UpdateConversationRequestLazyDelivery extends $pb.ProtobufEnum {
  static const UpdateConversationRequestLazyDelivery
      UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_UNSPECIFIED =
      UpdateConversationRequestLazyDelivery._(
          0,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_UNSPECIFIED');
  static const UpdateConversationRequestLazyDelivery
      UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_SERVER =
      UpdateConversationRequestLazyDelivery._(
          1,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_SERVER');
  static const UpdateConversationRequestLazyDelivery
      UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_ON =
      UpdateConversationRequestLazyDelivery._(2,
          _omitEnumNames ? '' : 'UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_ON');
  static const UpdateConversationRequestLazyDelivery
      UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_OFF =
      UpdateConversationRequestLazyDelivery._(
          3,
          _omitEnumNames
              ? ''
              : 'UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_OFF');

  static const $core.List<UpdateConversationRequestLazyDelivery> values =
      <UpdateConversationRequestLazyDelivery>[
    UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_UNSPECIFIED,
    UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_SERVER,
    UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_ON,
    UPDATE_CONVERSATION_REQUEST_LAZY_DELIVERY_OFF,
  ];

  static final $core.List<UpdateConversationRequestLazyDelivery?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static UpdateConversationRequestLazyDelivery? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdateConversationRequestLazyDelivery._(super.value, super.name);
}

class CreateChannelRequestAccess extends $pb.ProtobufEnum {
  static const CreateChannelRequestAccess
      CREATE_CHANNEL_REQUEST_ACCESS_UNSPECIFIED = CreateChannelRequestAccess._(
          0, _omitEnumNames ? '' : 'CREATE_CHANNEL_REQUEST_ACCESS_UNSPECIFIED');
  static const CreateChannelRequestAccess CREATE_CHANNEL_REQUEST_ACCESS_PUBLIC =
      CreateChannelRequestAccess._(
          1, _omitEnumNames ? '' : 'CREATE_CHANNEL_REQUEST_ACCESS_PUBLIC');
  static const CreateChannelRequestAccess CREATE_CHANNEL_REQUEST_ACCESS_KNOCK =
      CreateChannelRequestAccess._(
          2, _omitEnumNames ? '' : 'CREATE_CHANNEL_REQUEST_ACCESS_KNOCK');
  static const CreateChannelRequestAccess
      CREATE_CHANNEL_REQUEST_ACCESS_PRIVATE = CreateChannelRequestAccess._(
          3, _omitEnumNames ? '' : 'CREATE_CHANNEL_REQUEST_ACCESS_PRIVATE');

  static const $core.List<CreateChannelRequestAccess> values =
      <CreateChannelRequestAccess>[
    CREATE_CHANNEL_REQUEST_ACCESS_UNSPECIFIED,
    CREATE_CHANNEL_REQUEST_ACCESS_PUBLIC,
    CREATE_CHANNEL_REQUEST_ACCESS_KNOCK,
    CREATE_CHANNEL_REQUEST_ACCESS_PRIVATE,
  ];

  static final $core.List<CreateChannelRequestAccess?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CreateChannelRequestAccess? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CreateChannelRequestAccess._(super.value, super.name);
}

class CreateChannelRequestDefaultLevel extends $pb.ProtobufEnum {
  static const CreateChannelRequestDefaultLevel
      CREATE_CHANNEL_REQUEST_DEFAULT_LEVEL_UNSPECIFIED =
      CreateChannelRequestDefaultLevel._(
          0,
          _omitEnumNames
              ? ''
              : 'CREATE_CHANNEL_REQUEST_DEFAULT_LEVEL_UNSPECIFIED');
  static const CreateChannelRequestDefaultLevel
      CREATE_CHANNEL_REQUEST_DEFAULT_LEVEL_READER =
      CreateChannelRequestDefaultLevel._(1,
          _omitEnumNames ? '' : 'CREATE_CHANNEL_REQUEST_DEFAULT_LEVEL_READER');
  static const CreateChannelRequestDefaultLevel
      CREATE_CHANNEL_REQUEST_DEFAULT_LEVEL_MEMBER =
      CreateChannelRequestDefaultLevel._(2,
          _omitEnumNames ? '' : 'CREATE_CHANNEL_REQUEST_DEFAULT_LEVEL_MEMBER');

  static const $core.List<CreateChannelRequestDefaultLevel> values =
      <CreateChannelRequestDefaultLevel>[
    CREATE_CHANNEL_REQUEST_DEFAULT_LEVEL_UNSPECIFIED,
    CREATE_CHANNEL_REQUEST_DEFAULT_LEVEL_READER,
    CREATE_CHANNEL_REQUEST_DEFAULT_LEVEL_MEMBER,
  ];

  static final $core.List<CreateChannelRequestDefaultLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static CreateChannelRequestDefaultLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CreateChannelRequestDefaultLevel._(super.value, super.name);
}

class MemberLevel extends $pb.ProtobufEnum {
  static const MemberLevel MEMBER_LEVEL_UNSPECIFIED =
      MemberLevel._(0, _omitEnumNames ? '' : 'MEMBER_LEVEL_UNSPECIFIED');
  static const MemberLevel MEMBER_LEVEL_READER =
      MemberLevel._(1, _omitEnumNames ? '' : 'MEMBER_LEVEL_READER');
  static const MemberLevel MEMBER_LEVEL_MEMBER =
      MemberLevel._(2, _omitEnumNames ? '' : 'MEMBER_LEVEL_MEMBER');
  static const MemberLevel MEMBER_LEVEL_MANAGER =
      MemberLevel._(3, _omitEnumNames ? '' : 'MEMBER_LEVEL_MANAGER');

  static const $core.List<MemberLevel> values = <MemberLevel>[
    MEMBER_LEVEL_UNSPECIFIED,
    MEMBER_LEVEL_READER,
    MEMBER_LEVEL_MEMBER,
    MEMBER_LEVEL_MANAGER,
  ];

  static final $core.List<MemberLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static MemberLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MemberLevel._(super.value, super.name);
}

class MemberDirectLevel extends $pb.ProtobufEnum {
  static const MemberDirectLevel MEMBER_DIRECT_LEVEL_UNSPECIFIED =
      MemberDirectLevel._(
          0, _omitEnumNames ? '' : 'MEMBER_DIRECT_LEVEL_UNSPECIFIED');
  static const MemberDirectLevel MEMBER_DIRECT_LEVEL_READER =
      MemberDirectLevel._(
          1, _omitEnumNames ? '' : 'MEMBER_DIRECT_LEVEL_READER');
  static const MemberDirectLevel MEMBER_DIRECT_LEVEL_MEMBER =
      MemberDirectLevel._(
          2, _omitEnumNames ? '' : 'MEMBER_DIRECT_LEVEL_MEMBER');
  static const MemberDirectLevel MEMBER_DIRECT_LEVEL_MANAGER =
      MemberDirectLevel._(
          3, _omitEnumNames ? '' : 'MEMBER_DIRECT_LEVEL_MANAGER');

  static const $core.List<MemberDirectLevel> values = <MemberDirectLevel>[
    MEMBER_DIRECT_LEVEL_UNSPECIFIED,
    MEMBER_DIRECT_LEVEL_READER,
    MEMBER_DIRECT_LEVEL_MEMBER,
    MEMBER_DIRECT_LEVEL_MANAGER,
  ];

  static final $core.List<MemberDirectLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static MemberDirectLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MemberDirectLevel._(super.value, super.name);
}

class AddMembersRequestLevel extends $pb.ProtobufEnum {
  static const AddMembersRequestLevel ADD_MEMBERS_REQUEST_LEVEL_UNSPECIFIED =
      AddMembersRequestLevel._(
          0, _omitEnumNames ? '' : 'ADD_MEMBERS_REQUEST_LEVEL_UNSPECIFIED');
  static const AddMembersRequestLevel ADD_MEMBERS_REQUEST_LEVEL_READER =
      AddMembersRequestLevel._(
          1, _omitEnumNames ? '' : 'ADD_MEMBERS_REQUEST_LEVEL_READER');
  static const AddMembersRequestLevel ADD_MEMBERS_REQUEST_LEVEL_MEMBER =
      AddMembersRequestLevel._(
          2, _omitEnumNames ? '' : 'ADD_MEMBERS_REQUEST_LEVEL_MEMBER');
  static const AddMembersRequestLevel ADD_MEMBERS_REQUEST_LEVEL_MANAGER =
      AddMembersRequestLevel._(
          3, _omitEnumNames ? '' : 'ADD_MEMBERS_REQUEST_LEVEL_MANAGER');

  static const $core.List<AddMembersRequestLevel> values =
      <AddMembersRequestLevel>[
    ADD_MEMBERS_REQUEST_LEVEL_UNSPECIFIED,
    ADD_MEMBERS_REQUEST_LEVEL_READER,
    ADD_MEMBERS_REQUEST_LEVEL_MEMBER,
    ADD_MEMBERS_REQUEST_LEVEL_MANAGER,
  ];

  static final $core.List<AddMembersRequestLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static AddMembersRequestLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AddMembersRequestLevel._(super.value, super.name);
}

class SetMemberLevelRequestLevel extends $pb.ProtobufEnum {
  static const SetMemberLevelRequestLevel
      SET_MEMBER_LEVEL_REQUEST_LEVEL_UNSPECIFIED = SetMemberLevelRequestLevel._(
          0,
          _omitEnumNames ? '' : 'SET_MEMBER_LEVEL_REQUEST_LEVEL_UNSPECIFIED');
  static const SetMemberLevelRequestLevel
      SET_MEMBER_LEVEL_REQUEST_LEVEL_READER = SetMemberLevelRequestLevel._(
          1, _omitEnumNames ? '' : 'SET_MEMBER_LEVEL_REQUEST_LEVEL_READER');
  static const SetMemberLevelRequestLevel
      SET_MEMBER_LEVEL_REQUEST_LEVEL_MEMBER = SetMemberLevelRequestLevel._(
          2, _omitEnumNames ? '' : 'SET_MEMBER_LEVEL_REQUEST_LEVEL_MEMBER');
  static const SetMemberLevelRequestLevel
      SET_MEMBER_LEVEL_REQUEST_LEVEL_MANAGER = SetMemberLevelRequestLevel._(
          3, _omitEnumNames ? '' : 'SET_MEMBER_LEVEL_REQUEST_LEVEL_MANAGER');

  static const $core.List<SetMemberLevelRequestLevel> values =
      <SetMemberLevelRequestLevel>[
    SET_MEMBER_LEVEL_REQUEST_LEVEL_UNSPECIFIED,
    SET_MEMBER_LEVEL_REQUEST_LEVEL_READER,
    SET_MEMBER_LEVEL_REQUEST_LEVEL_MEMBER,
    SET_MEMBER_LEVEL_REQUEST_LEVEL_MANAGER,
  ];

  static final $core.List<SetMemberLevelRequestLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static SetMemberLevelRequestLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SetMemberLevelRequestLevel._(super.value, super.name);
}

class KnockOutcome extends $pb.ProtobufEnum {
  static const KnockOutcome KNOCK_OUTCOME_UNSPECIFIED =
      KnockOutcome._(0, _omitEnumNames ? '' : 'KNOCK_OUTCOME_UNSPECIFIED');
  static const KnockOutcome KNOCK_OUTCOME_PENDING =
      KnockOutcome._(1, _omitEnumNames ? '' : 'KNOCK_OUTCOME_PENDING');
  static const KnockOutcome KNOCK_OUTCOME_ACCEPTED =
      KnockOutcome._(2, _omitEnumNames ? '' : 'KNOCK_OUTCOME_ACCEPTED');
  static const KnockOutcome KNOCK_OUTCOME_DECLINED =
      KnockOutcome._(3, _omitEnumNames ? '' : 'KNOCK_OUTCOME_DECLINED');

  static const $core.List<KnockOutcome> values = <KnockOutcome>[
    KNOCK_OUTCOME_UNSPECIFIED,
    KNOCK_OUTCOME_PENDING,
    KNOCK_OUTCOME_ACCEPTED,
    KNOCK_OUTCOME_DECLINED,
  ];

  static final $core.List<KnockOutcome?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static KnockOutcome? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const KnockOutcome._(super.value, super.name);
}

class ChannelGroupLevel extends $pb.ProtobufEnum {
  static const ChannelGroupLevel CHANNEL_GROUP_LEVEL_UNSPECIFIED =
      ChannelGroupLevel._(
          0, _omitEnumNames ? '' : 'CHANNEL_GROUP_LEVEL_UNSPECIFIED');
  static const ChannelGroupLevel CHANNEL_GROUP_LEVEL_READER =
      ChannelGroupLevel._(
          1, _omitEnumNames ? '' : 'CHANNEL_GROUP_LEVEL_READER');
  static const ChannelGroupLevel CHANNEL_GROUP_LEVEL_MEMBER =
      ChannelGroupLevel._(
          2, _omitEnumNames ? '' : 'CHANNEL_GROUP_LEVEL_MEMBER');
  static const ChannelGroupLevel CHANNEL_GROUP_LEVEL_MANAGER =
      ChannelGroupLevel._(
          3, _omitEnumNames ? '' : 'CHANNEL_GROUP_LEVEL_MANAGER');

  static const $core.List<ChannelGroupLevel> values = <ChannelGroupLevel>[
    CHANNEL_GROUP_LEVEL_UNSPECIFIED,
    CHANNEL_GROUP_LEVEL_READER,
    CHANNEL_GROUP_LEVEL_MEMBER,
    CHANNEL_GROUP_LEVEL_MANAGER,
  ];

  static final $core.List<ChannelGroupLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ChannelGroupLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ChannelGroupLevel._(super.value, super.name);
}

class AddChannelGroupRequestLevel extends $pb.ProtobufEnum {
  static const AddChannelGroupRequestLevel
      ADD_CHANNEL_GROUP_REQUEST_LEVEL_UNSPECIFIED =
      AddChannelGroupRequestLevel._(0,
          _omitEnumNames ? '' : 'ADD_CHANNEL_GROUP_REQUEST_LEVEL_UNSPECIFIED');
  static const AddChannelGroupRequestLevel
      ADD_CHANNEL_GROUP_REQUEST_LEVEL_READER = AddChannelGroupRequestLevel._(
          1, _omitEnumNames ? '' : 'ADD_CHANNEL_GROUP_REQUEST_LEVEL_READER');
  static const AddChannelGroupRequestLevel
      ADD_CHANNEL_GROUP_REQUEST_LEVEL_MEMBER = AddChannelGroupRequestLevel._(
          2, _omitEnumNames ? '' : 'ADD_CHANNEL_GROUP_REQUEST_LEVEL_MEMBER');
  static const AddChannelGroupRequestLevel
      ADD_CHANNEL_GROUP_REQUEST_LEVEL_MANAGER = AddChannelGroupRequestLevel._(
          3, _omitEnumNames ? '' : 'ADD_CHANNEL_GROUP_REQUEST_LEVEL_MANAGER');

  static const $core.List<AddChannelGroupRequestLevel> values =
      <AddChannelGroupRequestLevel>[
    ADD_CHANNEL_GROUP_REQUEST_LEVEL_UNSPECIFIED,
    ADD_CHANNEL_GROUP_REQUEST_LEVEL_READER,
    ADD_CHANNEL_GROUP_REQUEST_LEVEL_MEMBER,
    ADD_CHANNEL_GROUP_REQUEST_LEVEL_MANAGER,
  ];

  static final $core.List<AddChannelGroupRequestLevel?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static AddChannelGroupRequestLevel? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AddChannelGroupRequestLevel._(super.value, super.name);
}

class UpdateMembershipRequestMode extends $pb.ProtobufEnum {
  static const UpdateMembershipRequestMode
      UPDATE_MEMBERSHIP_REQUEST_MODE_UNSPECIFIED =
      UpdateMembershipRequestMode._(0,
          _omitEnumNames ? '' : 'UPDATE_MEMBERSHIP_REQUEST_MODE_UNSPECIFIED');
  static const UpdateMembershipRequestMode UPDATE_MEMBERSHIP_REQUEST_MODE_ANY =
      UpdateMembershipRequestMode._(
          1, _omitEnumNames ? '' : 'UPDATE_MEMBERSHIP_REQUEST_MODE_ANY');
  static const UpdateMembershipRequestMode
      UPDATE_MEMBERSHIP_REQUEST_MODE_MENTIONS = UpdateMembershipRequestMode._(
          2, _omitEnumNames ? '' : 'UPDATE_MEMBERSHIP_REQUEST_MODE_MENTIONS');
  static const UpdateMembershipRequestMode
      UPDATE_MEMBERSHIP_REQUEST_MODE_NOTHING = UpdateMembershipRequestMode._(
          3, _omitEnumNames ? '' : 'UPDATE_MEMBERSHIP_REQUEST_MODE_NOTHING');

  static const $core.List<UpdateMembershipRequestMode> values =
      <UpdateMembershipRequestMode>[
    UPDATE_MEMBERSHIP_REQUEST_MODE_UNSPECIFIED,
    UPDATE_MEMBERSHIP_REQUEST_MODE_ANY,
    UPDATE_MEMBERSHIP_REQUEST_MODE_MENTIONS,
    UPDATE_MEMBERSHIP_REQUEST_MODE_NOTHING,
  ];

  static final $core.List<UpdateMembershipRequestMode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static UpdateMembershipRequestMode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdateMembershipRequestMode._(super.value, super.name);
}

class HandleReportRequestOutcome extends $pb.ProtobufEnum {
  static const HandleReportRequestOutcome
      HANDLE_REPORT_REQUEST_OUTCOME_UNSPECIFIED = HandleReportRequestOutcome._(
          0, _omitEnumNames ? '' : 'HANDLE_REPORT_REQUEST_OUTCOME_UNSPECIFIED');
  static const HandleReportRequestOutcome
      HANDLE_REPORT_REQUEST_OUTCOME_DISMISSED = HandleReportRequestOutcome._(
          1, _omitEnumNames ? '' : 'HANDLE_REPORT_REQUEST_OUTCOME_DISMISSED');
  static const HandleReportRequestOutcome
      HANDLE_REPORT_REQUEST_OUTCOME_DELETED = HandleReportRequestOutcome._(
          2, _omitEnumNames ? '' : 'HANDLE_REPORT_REQUEST_OUTCOME_DELETED');
  static const HandleReportRequestOutcome
      HANDLE_REPORT_REQUEST_OUTCOME_TIMED_OUT = HandleReportRequestOutcome._(
          3, _omitEnumNames ? '' : 'HANDLE_REPORT_REQUEST_OUTCOME_TIMED_OUT');

  static const $core.List<HandleReportRequestOutcome> values =
      <HandleReportRequestOutcome>[
    HANDLE_REPORT_REQUEST_OUTCOME_UNSPECIFIED,
    HANDLE_REPORT_REQUEST_OUTCOME_DISMISSED,
    HANDLE_REPORT_REQUEST_OUTCOME_DELETED,
    HANDLE_REPORT_REQUEST_OUTCOME_TIMED_OUT,
  ];

  static final $core.List<HandleReportRequestOutcome?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static HandleReportRequestOutcome? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const HandleReportRequestOutcome._(super.value, super.name);
}

class ReportSource extends $pb.ProtobufEnum {
  static const ReportSource REPORT_SOURCE_UNSPECIFIED =
      ReportSource._(0, _omitEnumNames ? '' : 'REPORT_SOURCE_UNSPECIFIED');
  static const ReportSource REPORT_SOURCE_PERSON =
      ReportSource._(1, _omitEnumNames ? '' : 'REPORT_SOURCE_PERSON');
  static const ReportSource REPORT_SOURCE_FILTER =
      ReportSource._(2, _omitEnumNames ? '' : 'REPORT_SOURCE_FILTER');

  static const $core.List<ReportSource> values = <ReportSource>[
    REPORT_SOURCE_UNSPECIFIED,
    REPORT_SOURCE_PERSON,
    REPORT_SOURCE_FILTER,
  ];

  static final $core.List<ReportSource?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ReportSource? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ReportSource._(super.value, super.name);
}

class ReportOutcome extends $pb.ProtobufEnum {
  static const ReportOutcome REPORT_OUTCOME_UNSPECIFIED =
      ReportOutcome._(0, _omitEnumNames ? '' : 'REPORT_OUTCOME_UNSPECIFIED');
  static const ReportOutcome REPORT_OUTCOME_DISMISSED =
      ReportOutcome._(1, _omitEnumNames ? '' : 'REPORT_OUTCOME_DISMISSED');
  static const ReportOutcome REPORT_OUTCOME_DELETED =
      ReportOutcome._(2, _omitEnumNames ? '' : 'REPORT_OUTCOME_DELETED');
  static const ReportOutcome REPORT_OUTCOME_TIMED_OUT =
      ReportOutcome._(3, _omitEnumNames ? '' : 'REPORT_OUTCOME_TIMED_OUT');

  static const $core.List<ReportOutcome> values = <ReportOutcome>[
    REPORT_OUTCOME_UNSPECIFIED,
    REPORT_OUTCOME_DISMISSED,
    REPORT_OUTCOME_DELETED,
    REPORT_OUTCOME_TIMED_OUT,
  ];

  static final $core.List<ReportOutcome?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ReportOutcome? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ReportOutcome._(super.value, super.name);
}

class ThreadStateState extends $pb.ProtobufEnum {
  static const ThreadStateState THREAD_STATE_STATE_UNSPECIFIED =
      ThreadStateState._(
          0, _omitEnumNames ? '' : 'THREAD_STATE_STATE_UNSPECIFIED');
  static const ThreadStateState THREAD_STATE_STATE_FOLLOWING =
      ThreadStateState._(
          1, _omitEnumNames ? '' : 'THREAD_STATE_STATE_FOLLOWING');
  static const ThreadStateState THREAD_STATE_STATE_IGNORED =
      ThreadStateState._(2, _omitEnumNames ? '' : 'THREAD_STATE_STATE_IGNORED');
  static const ThreadStateState THREAD_STATE_STATE_NONE =
      ThreadStateState._(3, _omitEnumNames ? '' : 'THREAD_STATE_STATE_NONE');

  static const $core.List<ThreadStateState> values = <ThreadStateState>[
    THREAD_STATE_STATE_UNSPECIFIED,
    THREAD_STATE_STATE_FOLLOWING,
    THREAD_STATE_STATE_IGNORED,
    THREAD_STATE_STATE_NONE,
  ];

  static final $core.List<ThreadStateState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ThreadStateState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ThreadStateState._(super.value, super.name);
}

class MessageVersionReason extends $pb.ProtobufEnum {
  static const MessageVersionReason MESSAGE_VERSION_REASON_UNSPECIFIED =
      MessageVersionReason._(
          0, _omitEnumNames ? '' : 'MESSAGE_VERSION_REASON_UNSPECIFIED');
  static const MessageVersionReason MESSAGE_VERSION_REASON_EDIT =
      MessageVersionReason._(
          1, _omitEnumNames ? '' : 'MESSAGE_VERSION_REASON_EDIT');
  static const MessageVersionReason MESSAGE_VERSION_REASON_DELETE =
      MessageVersionReason._(
          2, _omitEnumNames ? '' : 'MESSAGE_VERSION_REASON_DELETE');

  static const $core.List<MessageVersionReason> values = <MessageVersionReason>[
    MESSAGE_VERSION_REASON_UNSPECIFIED,
    MESSAGE_VERSION_REASON_EDIT,
    MESSAGE_VERSION_REASON_DELETE,
  ];

  static final $core.List<MessageVersionReason?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static MessageVersionReason? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MessageVersionReason._(super.value, super.name);
}

class PurgeMessageResponseState extends $pb.ProtobufEnum {
  static const PurgeMessageResponseState
      PURGE_MESSAGE_RESPONSE_STATE_UNSPECIFIED = PurgeMessageResponseState._(
          0, _omitEnumNames ? '' : 'PURGE_MESSAGE_RESPONSE_STATE_UNSPECIFIED');
  static const PurgeMessageResponseState PURGE_MESSAGE_RESPONSE_STATE_PURGED =
      PurgeMessageResponseState._(
          1, _omitEnumNames ? '' : 'PURGE_MESSAGE_RESPONSE_STATE_PURGED');
  static const PurgeMessageResponseState
      PURGE_MESSAGE_RESPONSE_STATE_REQUESTED = PurgeMessageResponseState._(
          2, _omitEnumNames ? '' : 'PURGE_MESSAGE_RESPONSE_STATE_REQUESTED');

  static const $core.List<PurgeMessageResponseState> values =
      <PurgeMessageResponseState>[
    PURGE_MESSAGE_RESPONSE_STATE_UNSPECIFIED,
    PURGE_MESSAGE_RESPONSE_STATE_PURGED,
    PURGE_MESSAGE_RESPONSE_STATE_REQUESTED,
  ];

  static final $core.List<PurgeMessageResponseState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static PurgeMessageResponseState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PurgeMessageResponseState._(super.value, super.name);
}

class SetThreadStateRequestState extends $pb.ProtobufEnum {
  static const SetThreadStateRequestState
      SET_THREAD_STATE_REQUEST_STATE_UNSPECIFIED = SetThreadStateRequestState._(
          0,
          _omitEnumNames ? '' : 'SET_THREAD_STATE_REQUEST_STATE_UNSPECIFIED');
  static const SetThreadStateRequestState
      SET_THREAD_STATE_REQUEST_STATE_FOLLOWING = SetThreadStateRequestState._(
          1, _omitEnumNames ? '' : 'SET_THREAD_STATE_REQUEST_STATE_FOLLOWING');
  static const SetThreadStateRequestState
      SET_THREAD_STATE_REQUEST_STATE_IGNORED = SetThreadStateRequestState._(
          2, _omitEnumNames ? '' : 'SET_THREAD_STATE_REQUEST_STATE_IGNORED');
  static const SetThreadStateRequestState SET_THREAD_STATE_REQUEST_STATE_NONE =
      SetThreadStateRequestState._(
          3, _omitEnumNames ? '' : 'SET_THREAD_STATE_REQUEST_STATE_NONE');

  static const $core.List<SetThreadStateRequestState> values =
      <SetThreadStateRequestState>[
    SET_THREAD_STATE_REQUEST_STATE_UNSPECIFIED,
    SET_THREAD_STATE_REQUEST_STATE_FOLLOWING,
    SET_THREAD_STATE_REQUEST_STATE_IGNORED,
    SET_THREAD_STATE_REQUEST_STATE_NONE,
  ];

  static final $core.List<SetThreadStateRequestState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static SetThreadStateRequestState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SetThreadStateRequestState._(super.value, super.name);
}

class ScheduledInputKind extends $pb.ProtobufEnum {
  static const ScheduledInputKind SCHEDULED_INPUT_KIND_UNSPECIFIED =
      ScheduledInputKind._(
          0, _omitEnumNames ? '' : 'SCHEDULED_INPUT_KIND_UNSPECIFIED');
  static const ScheduledInputKind SCHEDULED_INPUT_KIND_MESSAGE =
      ScheduledInputKind._(
          1, _omitEnumNames ? '' : 'SCHEDULED_INPUT_KIND_MESSAGE');
  static const ScheduledInputKind SCHEDULED_INPUT_KIND_REMINDER =
      ScheduledInputKind._(
          2, _omitEnumNames ? '' : 'SCHEDULED_INPUT_KIND_REMINDER');
  static const ScheduledInputKind SCHEDULED_INPUT_KIND_FORM =
      ScheduledInputKind._(
          3, _omitEnumNames ? '' : 'SCHEDULED_INPUT_KIND_FORM');

  static const $core.List<ScheduledInputKind> values = <ScheduledInputKind>[
    SCHEDULED_INPUT_KIND_UNSPECIFIED,
    SCHEDULED_INPUT_KIND_MESSAGE,
    SCHEDULED_INPUT_KIND_REMINDER,
    SCHEDULED_INPUT_KIND_FORM,
  ];

  static final $core.List<ScheduledInputKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ScheduledInputKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ScheduledInputKind._(super.value, super.name);
}

class CommandKind extends $pb.ProtobufEnum {
  static const CommandKind COMMAND_KIND_UNSPECIFIED =
      CommandKind._(0, _omitEnumNames ? '' : 'COMMAND_KIND_UNSPECIFIED');
  static const CommandKind COMMAND_KIND_BUILTIN =
      CommandKind._(1, _omitEnumNames ? '' : 'COMMAND_KIND_BUILTIN');
  static const CommandKind COMMAND_KIND_SAVED_REPLY =
      CommandKind._(2, _omitEnumNames ? '' : 'COMMAND_KIND_SAVED_REPLY');
  static const CommandKind COMMAND_KIND_BOT =
      CommandKind._(3, _omitEnumNames ? '' : 'COMMAND_KIND_BOT');

  static const $core.List<CommandKind> values = <CommandKind>[
    COMMAND_KIND_UNSPECIFIED,
    COMMAND_KIND_BUILTIN,
    COMMAND_KIND_SAVED_REPLY,
    COMMAND_KIND_BOT,
  ];

  static final $core.List<CommandKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CommandKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CommandKind._(super.value, super.name);
}

class WebhookState extends $pb.ProtobufEnum {
  static const WebhookState WEBHOOK_STATE_UNSPECIFIED =
      WebhookState._(0, _omitEnumNames ? '' : 'WEBHOOK_STATE_UNSPECIFIED');
  static const WebhookState WEBHOOK_STATE_WAITING =
      WebhookState._(1, _omitEnumNames ? '' : 'WEBHOOK_STATE_WAITING');
  static const WebhookState WEBHOOK_STATE_APPROVED =
      WebhookState._(2, _omitEnumNames ? '' : 'WEBHOOK_STATE_APPROVED');
  static const WebhookState WEBHOOK_STATE_DECLINED =
      WebhookState._(3, _omitEnumNames ? '' : 'WEBHOOK_STATE_DECLINED');
  static const WebhookState WEBHOOK_STATE_REVOKED =
      WebhookState._(4, _omitEnumNames ? '' : 'WEBHOOK_STATE_REVOKED');

  static const $core.List<WebhookState> values = <WebhookState>[
    WEBHOOK_STATE_UNSPECIFIED,
    WEBHOOK_STATE_WAITING,
    WEBHOOK_STATE_APPROVED,
    WEBHOOK_STATE_DECLINED,
    WEBHOOK_STATE_REVOKED,
  ];

  static final $core.List<WebhookState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static WebhookState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const WebhookState._(super.value, super.name);
}

class InteractionResponseType extends $pb.ProtobufEnum {
  static const InteractionResponseType INTERACTION_RESPONSE_TYPE_UNSPECIFIED =
      InteractionResponseType._(
          0, _omitEnumNames ? '' : 'INTERACTION_RESPONSE_TYPE_UNSPECIFIED');
  static const InteractionResponseType INTERACTION_RESPONSE_TYPE_MESSAGE =
      InteractionResponseType._(
          1, _omitEnumNames ? '' : 'INTERACTION_RESPONSE_TYPE_MESSAGE');
  static const InteractionResponseType INTERACTION_RESPONSE_TYPE_PRIVATE =
      InteractionResponseType._(
          2, _omitEnumNames ? '' : 'INTERACTION_RESPONSE_TYPE_PRIVATE');
  static const InteractionResponseType INTERACTION_RESPONSE_TYPE_DIALOG =
      InteractionResponseType._(
          3, _omitEnumNames ? '' : 'INTERACTION_RESPONSE_TYPE_DIALOG');
  static const InteractionResponseType INTERACTION_RESPONSE_TYPE_NONE =
      InteractionResponseType._(
          4, _omitEnumNames ? '' : 'INTERACTION_RESPONSE_TYPE_NONE');

  static const $core.List<InteractionResponseType> values =
      <InteractionResponseType>[
    INTERACTION_RESPONSE_TYPE_UNSPECIFIED,
    INTERACTION_RESPONSE_TYPE_MESSAGE,
    INTERACTION_RESPONSE_TYPE_PRIVATE,
    INTERACTION_RESPONSE_TYPE_DIALOG,
    INTERACTION_RESPONSE_TYPE_NONE,
  ];

  static final $core.List<InteractionResponseType?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static InteractionResponseType? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const InteractionResponseType._(super.value, super.name);
}

class WebhookCardColor extends $pb.ProtobufEnum {
  static const WebhookCardColor WEBHOOK_CARD_COLOR_UNSPECIFIED =
      WebhookCardColor._(
          0, _omitEnumNames ? '' : 'WEBHOOK_CARD_COLOR_UNSPECIFIED');
  static const WebhookCardColor WEBHOOK_CARD_COLOR_LAVENDER =
      WebhookCardColor._(
          1, _omitEnumNames ? '' : 'WEBHOOK_CARD_COLOR_LAVENDER');
  static const WebhookCardColor WEBHOOK_CARD_COLOR_CYAN =
      WebhookCardColor._(2, _omitEnumNames ? '' : 'WEBHOOK_CARD_COLOR_CYAN');
  static const WebhookCardColor WEBHOOK_CARD_COLOR_PINK =
      WebhookCardColor._(3, _omitEnumNames ? '' : 'WEBHOOK_CARD_COLOR_PINK');
  static const WebhookCardColor WEBHOOK_CARD_COLOR_YELLOW =
      WebhookCardColor._(4, _omitEnumNames ? '' : 'WEBHOOK_CARD_COLOR_YELLOW');
  static const WebhookCardColor WEBHOOK_CARD_COLOR_GREEN =
      WebhookCardColor._(5, _omitEnumNames ? '' : 'WEBHOOK_CARD_COLOR_GREEN');
  static const WebhookCardColor WEBHOOK_CARD_COLOR_RED =
      WebhookCardColor._(6, _omitEnumNames ? '' : 'WEBHOOK_CARD_COLOR_RED');
  static const WebhookCardColor WEBHOOK_CARD_COLOR_GREY =
      WebhookCardColor._(7, _omitEnumNames ? '' : 'WEBHOOK_CARD_COLOR_GREY');

  static const $core.List<WebhookCardColor> values = <WebhookCardColor>[
    WEBHOOK_CARD_COLOR_UNSPECIFIED,
    WEBHOOK_CARD_COLOR_LAVENDER,
    WEBHOOK_CARD_COLOR_CYAN,
    WEBHOOK_CARD_COLOR_PINK,
    WEBHOOK_CARD_COLOR_YELLOW,
    WEBHOOK_CARD_COLOR_GREEN,
    WEBHOOK_CARD_COLOR_RED,
    WEBHOOK_CARD_COLOR_GREY,
  ];

  static final $core.List<WebhookCardColor?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static WebhookCardColor? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const WebhookCardColor._(super.value, super.name);
}

class PostEpochRecordResponse409ErrorCode extends $pb.ProtobufEnum {
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_UNSPECIFIED =
      PostEpochRecordResponse409ErrorCode._(
          0,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_UNSPECIFIED');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_INVALID_REQUEST =
      PostEpochRecordResponse409ErrorCode._(
          1,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_INVALID_REQUEST');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_INVALID_VALUE =
      PostEpochRecordResponse409ErrorCode._(
          2,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_INVALID_VALUE');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_UNAUTHENTICATED =
      PostEpochRecordResponse409ErrorCode._(
          3,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_UNAUTHENTICATED');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_SECOND_FACTOR_REQUIRED =
      PostEpochRecordResponse409ErrorCode._(
          4,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_SECOND_FACTOR_REQUIRED');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_FORBIDDEN =
      PostEpochRecordResponse409ErrorCode._(
          5,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_FORBIDDEN');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_TIMED_OUT =
      PostEpochRecordResponse409ErrorCode._(
          6,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_TIMED_OUT');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_ARCHIVED =
      PostEpochRecordResponse409ErrorCode._(
          7,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_ARCHIVED');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_LOCKED =
      PostEpochRecordResponse409ErrorCode._(
          8,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_LOCKED');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_FEATURE_OFF =
      PostEpochRecordResponse409ErrorCode._(
          9,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_FEATURE_OFF');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_FRESH_CHECK_REQUIRED =
      PostEpochRecordResponse409ErrorCode._(
          10,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_FRESH_CHECK_REQUIRED');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_NOT_FOUND =
      PostEpochRecordResponse409ErrorCode._(
          11,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_NOT_FOUND');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_CONFLICT =
      PostEpochRecordResponse409ErrorCode._(
          12,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_CONFLICT');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_NAME_TAKEN =
      PostEpochRecordResponse409ErrorCode._(
          13,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_NAME_TAKEN');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_ALREADY_EXISTS =
      PostEpochRecordResponse409ErrorCode._(
          14,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_ALREADY_EXISTS');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_TOO_LARGE =
      PostEpochRecordResponse409ErrorCode._(
          15,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_TOO_LARGE');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_BLOCKED_BY_FILTER =
      PostEpochRecordResponse409ErrorCode._(
          16,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_BLOCKED_BY_FILTER');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_RATE_LIMITED =
      PostEpochRecordResponse409ErrorCode._(
          17,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_RATE_LIMITED');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_SLOW_MODE =
      PostEpochRecordResponse409ErrorCode._(
          18,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_SLOW_MODE');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_INTERNAL =
      PostEpochRecordResponse409ErrorCode._(
          19,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_INTERNAL');
  static const PostEpochRecordResponse409ErrorCode
      POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_UNAVAILABLE =
      PostEpochRecordResponse409ErrorCode._(
          20,
          _omitEnumNames
              ? ''
              : 'POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_UNAVAILABLE');

  static const $core.List<PostEpochRecordResponse409ErrorCode> values =
      <PostEpochRecordResponse409ErrorCode>[
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_UNSPECIFIED,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_INVALID_REQUEST,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_INVALID_VALUE,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_UNAUTHENTICATED,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_SECOND_FACTOR_REQUIRED,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_FORBIDDEN,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_TIMED_OUT,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_ARCHIVED,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_LOCKED,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_FEATURE_OFF,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_FRESH_CHECK_REQUIRED,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_NOT_FOUND,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_CONFLICT,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_NAME_TAKEN,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_ALREADY_EXISTS,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_TOO_LARGE,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_BLOCKED_BY_FILTER,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_RATE_LIMITED,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_SLOW_MODE,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_INTERNAL,
    POST_EPOCH_RECORD_RESPONSE409_ERROR_CODE_UNAVAILABLE,
  ];

  static final $core.List<PostEpochRecordResponse409ErrorCode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 20);
  static PostEpochRecordResponse409ErrorCode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PostEpochRecordResponse409ErrorCode._(super.value, super.name);
}

class LockedCopyKind extends $pb.ProtobufEnum {
  static const LockedCopyKind LOCKED_COPY_KIND_UNSPECIFIED =
      LockedCopyKind._(0, _omitEnumNames ? '' : 'LOCKED_COPY_KIND_UNSPECIFIED');
  static const LockedCopyKind LOCKED_COPY_KIND_PASSKEY =
      LockedCopyKind._(1, _omitEnumNames ? '' : 'LOCKED_COPY_KIND_PASSKEY');
  static const LockedCopyKind LOCKED_COPY_KIND_PHRASE =
      LockedCopyKind._(2, _omitEnumNames ? '' : 'LOCKED_COPY_KIND_PHRASE');
  static const LockedCopyKind LOCKED_COPY_KIND_DEVICE =
      LockedCopyKind._(3, _omitEnumNames ? '' : 'LOCKED_COPY_KIND_DEVICE');

  static const $core.List<LockedCopyKind> values = <LockedCopyKind>[
    LOCKED_COPY_KIND_UNSPECIFIED,
    LOCKED_COPY_KIND_PASSKEY,
    LOCKED_COPY_KIND_PHRASE,
    LOCKED_COPY_KIND_DEVICE,
  ];

  static final $core.List<LockedCopyKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static LockedCopyKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const LockedCopyKind._(super.value, super.name);
}

class LockedCopyInputKind extends $pb.ProtobufEnum {
  static const LockedCopyInputKind LOCKED_COPY_INPUT_KIND_UNSPECIFIED =
      LockedCopyInputKind._(
          0, _omitEnumNames ? '' : 'LOCKED_COPY_INPUT_KIND_UNSPECIFIED');
  static const LockedCopyInputKind LOCKED_COPY_INPUT_KIND_PASSKEY =
      LockedCopyInputKind._(
          1, _omitEnumNames ? '' : 'LOCKED_COPY_INPUT_KIND_PASSKEY');
  static const LockedCopyInputKind LOCKED_COPY_INPUT_KIND_PHRASE =
      LockedCopyInputKind._(
          2, _omitEnumNames ? '' : 'LOCKED_COPY_INPUT_KIND_PHRASE');
  static const LockedCopyInputKind LOCKED_COPY_INPUT_KIND_DEVICE =
      LockedCopyInputKind._(
          3, _omitEnumNames ? '' : 'LOCKED_COPY_INPUT_KIND_DEVICE');

  static const $core.List<LockedCopyInputKind> values = <LockedCopyInputKind>[
    LOCKED_COPY_INPUT_KIND_UNSPECIFIED,
    LOCKED_COPY_INPUT_KIND_PASSKEY,
    LOCKED_COPY_INPUT_KIND_PHRASE,
    LOCKED_COPY_INPUT_KIND_DEVICE,
  ];

  static final $core.List<LockedCopyInputKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static LockedCopyInputKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const LockedCopyInputKind._(super.value, super.name);
}

class SettingsRegistrationMode extends $pb.ProtobufEnum {
  static const SettingsRegistrationMode SETTINGS_REGISTRATION_MODE_UNSPECIFIED =
      SettingsRegistrationMode._(
          0, _omitEnumNames ? '' : 'SETTINGS_REGISTRATION_MODE_UNSPECIFIED');
  static const SettingsRegistrationMode SETTINGS_REGISTRATION_MODE_OPEN =
      SettingsRegistrationMode._(
          1, _omitEnumNames ? '' : 'SETTINGS_REGISTRATION_MODE_OPEN');
  static const SettingsRegistrationMode SETTINGS_REGISTRATION_MODE_INVITE =
      SettingsRegistrationMode._(
          2, _omitEnumNames ? '' : 'SETTINGS_REGISTRATION_MODE_INVITE');
  static const SettingsRegistrationMode SETTINGS_REGISTRATION_MODE_CLOSED =
      SettingsRegistrationMode._(
          3, _omitEnumNames ? '' : 'SETTINGS_REGISTRATION_MODE_CLOSED');

  static const $core.List<SettingsRegistrationMode> values =
      <SettingsRegistrationMode>[
    SETTINGS_REGISTRATION_MODE_UNSPECIFIED,
    SETTINGS_REGISTRATION_MODE_OPEN,
    SETTINGS_REGISTRATION_MODE_INVITE,
    SETTINGS_REGISTRATION_MODE_CLOSED,
  ];

  static final $core.List<SettingsRegistrationMode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static SettingsRegistrationMode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SettingsRegistrationMode._(super.value, super.name);
}

class SettingsAuthMemberSignIn extends $pb.ProtobufEnum {
  static const SettingsAuthMemberSignIn
      SETTINGS_AUTH_MEMBER_SIGN_IN_UNSPECIFIED = SettingsAuthMemberSignIn._(
          0, _omitEnumNames ? '' : 'SETTINGS_AUTH_MEMBER_SIGN_IN_UNSPECIFIED');
  static const SettingsAuthMemberSignIn SETTINGS_AUTH_MEMBER_SIGN_IN_PASSKEY =
      SettingsAuthMemberSignIn._(
          1, _omitEnumNames ? '' : 'SETTINGS_AUTH_MEMBER_SIGN_IN_PASSKEY');
  static const SettingsAuthMemberSignIn SETTINGS_AUTH_MEMBER_SIGN_IN_EITHER =
      SettingsAuthMemberSignIn._(
          2, _omitEnumNames ? '' : 'SETTINGS_AUTH_MEMBER_SIGN_IN_EITHER');
  static const SettingsAuthMemberSignIn SETTINGS_AUTH_MEMBER_SIGN_IN_SSO =
      SettingsAuthMemberSignIn._(
          3, _omitEnumNames ? '' : 'SETTINGS_AUTH_MEMBER_SIGN_IN_SSO');

  static const $core.List<SettingsAuthMemberSignIn> values =
      <SettingsAuthMemberSignIn>[
    SETTINGS_AUTH_MEMBER_SIGN_IN_UNSPECIFIED,
    SETTINGS_AUTH_MEMBER_SIGN_IN_PASSKEY,
    SETTINGS_AUTH_MEMBER_SIGN_IN_EITHER,
    SETTINGS_AUTH_MEMBER_SIGN_IN_SSO,
  ];

  static final $core.List<SettingsAuthMemberSignIn?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static SettingsAuthMemberSignIn? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SettingsAuthMemberSignIn._(super.value, super.name);
}

class SettingsAuthSecondFactor extends $pb.ProtobufEnum {
  static const SettingsAuthSecondFactor
      SETTINGS_AUTH_SECOND_FACTOR_UNSPECIFIED = SettingsAuthSecondFactor._(
          0, _omitEnumNames ? '' : 'SETTINGS_AUTH_SECOND_FACTOR_UNSPECIFIED');
  static const SettingsAuthSecondFactor SETTINGS_AUTH_SECOND_FACTOR_OFF =
      SettingsAuthSecondFactor._(
          1, _omitEnumNames ? '' : 'SETTINGS_AUTH_SECOND_FACTOR_OFF');
  static const SettingsAuthSecondFactor SETTINGS_AUTH_SECOND_FACTOR_REQUIRED =
      SettingsAuthSecondFactor._(
          2, _omitEnumNames ? '' : 'SETTINGS_AUTH_SECOND_FACTOR_REQUIRED');

  static const $core.List<SettingsAuthSecondFactor> values =
      <SettingsAuthSecondFactor>[
    SETTINGS_AUTH_SECOND_FACTOR_UNSPECIFIED,
    SETTINGS_AUTH_SECOND_FACTOR_OFF,
    SETTINGS_AUTH_SECOND_FACTOR_REQUIRED,
  ];

  static final $core.List<SettingsAuthSecondFactor?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static SettingsAuthSecondFactor? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SettingsAuthSecondFactor._(super.value, super.name);
}

class SettingsAuthSecondFactorAccepted extends $pb.ProtobufEnum {
  static const SettingsAuthSecondFactorAccepted
      SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_UNSPECIFIED =
      SettingsAuthSecondFactorAccepted._(
          0,
          _omitEnumNames
              ? ''
              : 'SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_UNSPECIFIED');
  static const SettingsAuthSecondFactorAccepted
      SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_PASSKEY =
      SettingsAuthSecondFactorAccepted._(1,
          _omitEnumNames ? '' : 'SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_PASSKEY');
  static const SettingsAuthSecondFactorAccepted
      SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_TOTP =
      SettingsAuthSecondFactorAccepted._(
          2, _omitEnumNames ? '' : 'SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_TOTP');
  static const SettingsAuthSecondFactorAccepted
      SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_PROVIDER =
      SettingsAuthSecondFactorAccepted._(
          3,
          _omitEnumNames
              ? ''
              : 'SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_PROVIDER');

  static const $core.List<SettingsAuthSecondFactorAccepted> values =
      <SettingsAuthSecondFactorAccepted>[
    SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_UNSPECIFIED,
    SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_PASSKEY,
    SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_TOTP,
    SETTINGS_AUTH_SECOND_FACTOR_ACCEPTED_PROVIDER,
  ];

  static final $core.List<SettingsAuthSecondFactorAccepted?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static SettingsAuthSecondFactorAccepted? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SettingsAuthSecondFactorAccepted._(super.value, super.name);
}

class SettingsFilesStore extends $pb.ProtobufEnum {
  static const SettingsFilesStore SETTINGS_FILES_STORE_UNSPECIFIED =
      SettingsFilesStore._(
          0, _omitEnumNames ? '' : 'SETTINGS_FILES_STORE_UNSPECIFIED');
  static const SettingsFilesStore SETTINGS_FILES_STORE_DISK =
      SettingsFilesStore._(
          1, _omitEnumNames ? '' : 'SETTINGS_FILES_STORE_DISK');
  static const SettingsFilesStore SETTINGS_FILES_STORE_S3 =
      SettingsFilesStore._(2, _omitEnumNames ? '' : 'SETTINGS_FILES_STORE_S3');

  static const $core.List<SettingsFilesStore> values = <SettingsFilesStore>[
    SETTINGS_FILES_STORE_UNSPECIFIED,
    SETTINGS_FILES_STORE_DISK,
    SETTINGS_FILES_STORE_S3,
  ];

  static final $core.List<SettingsFilesStore?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static SettingsFilesStore? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SettingsFilesStore._(super.value, super.name);
}

class SsoProviderKind extends $pb.ProtobufEnum {
  static const SsoProviderKind SSO_PROVIDER_KIND_UNSPECIFIED =
      SsoProviderKind._(
          0, _omitEnumNames ? '' : 'SSO_PROVIDER_KIND_UNSPECIFIED');
  static const SsoProviderKind SSO_PROVIDER_KIND_OIDC =
      SsoProviderKind._(1, _omitEnumNames ? '' : 'SSO_PROVIDER_KIND_OIDC');
  static const SsoProviderKind SSO_PROVIDER_KIND_SAML =
      SsoProviderKind._(2, _omitEnumNames ? '' : 'SSO_PROVIDER_KIND_SAML');

  static const $core.List<SsoProviderKind> values = <SsoProviderKind>[
    SSO_PROVIDER_KIND_UNSPECIFIED,
    SSO_PROVIDER_KIND_OIDC,
    SSO_PROVIDER_KIND_SAML,
  ];

  static final $core.List<SsoProviderKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static SsoProviderKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SsoProviderKind._(super.value, super.name);
}

class SsoProviderInputKind extends $pb.ProtobufEnum {
  static const SsoProviderInputKind SSO_PROVIDER_INPUT_KIND_UNSPECIFIED =
      SsoProviderInputKind._(
          0, _omitEnumNames ? '' : 'SSO_PROVIDER_INPUT_KIND_UNSPECIFIED');
  static const SsoProviderInputKind SSO_PROVIDER_INPUT_KIND_OIDC =
      SsoProviderInputKind._(
          1, _omitEnumNames ? '' : 'SSO_PROVIDER_INPUT_KIND_OIDC');
  static const SsoProviderInputKind SSO_PROVIDER_INPUT_KIND_SAML =
      SsoProviderInputKind._(
          2, _omitEnumNames ? '' : 'SSO_PROVIDER_INPUT_KIND_SAML');

  static const $core.List<SsoProviderInputKind> values = <SsoProviderInputKind>[
    SSO_PROVIDER_INPUT_KIND_UNSPECIFIED,
    SSO_PROVIDER_INPUT_KIND_OIDC,
    SSO_PROVIDER_INPUT_KIND_SAML,
  ];

  static final $core.List<SsoProviderInputKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static SsoProviderInputKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SsoProviderInputKind._(super.value, super.name);
}

class InviteRole extends $pb.ProtobufEnum {
  static const InviteRole INVITE_ROLE_UNSPECIFIED =
      InviteRole._(0, _omitEnumNames ? '' : 'INVITE_ROLE_UNSPECIFIED');
  static const InviteRole INVITE_ROLE_MEMBER =
      InviteRole._(1, _omitEnumNames ? '' : 'INVITE_ROLE_MEMBER');
  static const InviteRole INVITE_ROLE_GUEST =
      InviteRole._(2, _omitEnumNames ? '' : 'INVITE_ROLE_GUEST');

  static const $core.List<InviteRole> values = <InviteRole>[
    INVITE_ROLE_UNSPECIFIED,
    INVITE_ROLE_MEMBER,
    INVITE_ROLE_GUEST,
  ];

  static final $core.List<InviteRole?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static InviteRole? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const InviteRole._(super.value, super.name);
}

class CreateInviteRequestRole extends $pb.ProtobufEnum {
  static const CreateInviteRequestRole CREATE_INVITE_REQUEST_ROLE_UNSPECIFIED =
      CreateInviteRequestRole._(
          0, _omitEnumNames ? '' : 'CREATE_INVITE_REQUEST_ROLE_UNSPECIFIED');
  static const CreateInviteRequestRole CREATE_INVITE_REQUEST_ROLE_MEMBER =
      CreateInviteRequestRole._(
          1, _omitEnumNames ? '' : 'CREATE_INVITE_REQUEST_ROLE_MEMBER');
  static const CreateInviteRequestRole CREATE_INVITE_REQUEST_ROLE_GUEST =
      CreateInviteRequestRole._(
          2, _omitEnumNames ? '' : 'CREATE_INVITE_REQUEST_ROLE_GUEST');

  static const $core.List<CreateInviteRequestRole> values =
      <CreateInviteRequestRole>[
    CREATE_INVITE_REQUEST_ROLE_UNSPECIFIED,
    CREATE_INVITE_REQUEST_ROLE_MEMBER,
    CREATE_INVITE_REQUEST_ROLE_GUEST,
  ];

  static final $core.List<CreateInviteRequestRole?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static CreateInviteRequestRole? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CreateInviteRequestRole._(super.value, super.name);
}

class AdminAccountRole extends $pb.ProtobufEnum {
  static const AdminAccountRole ADMIN_ACCOUNT_ROLE_UNSPECIFIED =
      AdminAccountRole._(
          0, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_ROLE_UNSPECIFIED');
  static const AdminAccountRole ADMIN_ACCOUNT_ROLE_OWNER =
      AdminAccountRole._(1, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_ROLE_OWNER');
  static const AdminAccountRole ADMIN_ACCOUNT_ROLE_ADMIN =
      AdminAccountRole._(2, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_ROLE_ADMIN');
  static const AdminAccountRole ADMIN_ACCOUNT_ROLE_MEMBER =
      AdminAccountRole._(3, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_ROLE_MEMBER');
  static const AdminAccountRole ADMIN_ACCOUNT_ROLE_GUEST =
      AdminAccountRole._(4, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_ROLE_GUEST');
  static const AdminAccountRole ADMIN_ACCOUNT_ROLE_BOT =
      AdminAccountRole._(5, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_ROLE_BOT');
  static const AdminAccountRole ADMIN_ACCOUNT_ROLE_REMOTE =
      AdminAccountRole._(6, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_ROLE_REMOTE');

  static const $core.List<AdminAccountRole> values = <AdminAccountRole>[
    ADMIN_ACCOUNT_ROLE_UNSPECIFIED,
    ADMIN_ACCOUNT_ROLE_OWNER,
    ADMIN_ACCOUNT_ROLE_ADMIN,
    ADMIN_ACCOUNT_ROLE_MEMBER,
    ADMIN_ACCOUNT_ROLE_GUEST,
    ADMIN_ACCOUNT_ROLE_BOT,
    ADMIN_ACCOUNT_ROLE_REMOTE,
  ];

  static final $core.List<AdminAccountRole?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static AdminAccountRole? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AdminAccountRole._(super.value, super.name);
}

class AdminAccountPresence extends $pb.ProtobufEnum {
  static const AdminAccountPresence ADMIN_ACCOUNT_PRESENCE_UNSPECIFIED =
      AdminAccountPresence._(
          0, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_PRESENCE_UNSPECIFIED');
  static const AdminAccountPresence ADMIN_ACCOUNT_PRESENCE_ONLINE =
      AdminAccountPresence._(
          1, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_PRESENCE_ONLINE');
  static const AdminAccountPresence ADMIN_ACCOUNT_PRESENCE_AWAY =
      AdminAccountPresence._(
          2, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_PRESENCE_AWAY');
  static const AdminAccountPresence ADMIN_ACCOUNT_PRESENCE_OFFLINE =
      AdminAccountPresence._(
          3, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_PRESENCE_OFFLINE');

  static const $core.List<AdminAccountPresence> values = <AdminAccountPresence>[
    ADMIN_ACCOUNT_PRESENCE_UNSPECIFIED,
    ADMIN_ACCOUNT_PRESENCE_ONLINE,
    ADMIN_ACCOUNT_PRESENCE_AWAY,
    ADMIN_ACCOUNT_PRESENCE_OFFLINE,
  ];

  static final $core.List<AdminAccountPresence?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static AdminAccountPresence? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AdminAccountPresence._(super.value, super.name);
}

class AdminAccountLastSeen extends $pb.ProtobufEnum {
  static const AdminAccountLastSeen ADMIN_ACCOUNT_LAST_SEEN_UNSPECIFIED =
      AdminAccountLastSeen._(
          0, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_LAST_SEEN_UNSPECIFIED');
  static const AdminAccountLastSeen ADMIN_ACCOUNT_LAST_SEEN_ONLINE =
      AdminAccountLastSeen._(
          1, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_LAST_SEEN_ONLINE');
  static const AdminAccountLastSeen ADMIN_ACCOUNT_LAST_SEEN_RECENTLY =
      AdminAccountLastSeen._(
          2, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_LAST_SEEN_RECENTLY');
  static const AdminAccountLastSeen ADMIN_ACCOUNT_LAST_SEEN_WITHIN_WEEK =
      AdminAccountLastSeen._(
          3, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_LAST_SEEN_WITHIN_WEEK');
  static const AdminAccountLastSeen ADMIN_ACCOUNT_LAST_SEEN_LONG_AGO =
      AdminAccountLastSeen._(
          4, _omitEnumNames ? '' : 'ADMIN_ACCOUNT_LAST_SEEN_LONG_AGO');

  static const $core.List<AdminAccountLastSeen> values = <AdminAccountLastSeen>[
    ADMIN_ACCOUNT_LAST_SEEN_UNSPECIFIED,
    ADMIN_ACCOUNT_LAST_SEEN_ONLINE,
    ADMIN_ACCOUNT_LAST_SEEN_RECENTLY,
    ADMIN_ACCOUNT_LAST_SEEN_WITHIN_WEEK,
    ADMIN_ACCOUNT_LAST_SEEN_LONG_AGO,
  ];

  static final $core.List<AdminAccountLastSeen?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static AdminAccountLastSeen? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AdminAccountLastSeen._(super.value, super.name);
}

class SetAccountRoleRequestRole extends $pb.ProtobufEnum {
  static const SetAccountRoleRequestRole
      SET_ACCOUNT_ROLE_REQUEST_ROLE_UNSPECIFIED = SetAccountRoleRequestRole._(
          0, _omitEnumNames ? '' : 'SET_ACCOUNT_ROLE_REQUEST_ROLE_UNSPECIFIED');
  static const SetAccountRoleRequestRole SET_ACCOUNT_ROLE_REQUEST_ROLE_OWNER =
      SetAccountRoleRequestRole._(
          1, _omitEnumNames ? '' : 'SET_ACCOUNT_ROLE_REQUEST_ROLE_OWNER');
  static const SetAccountRoleRequestRole SET_ACCOUNT_ROLE_REQUEST_ROLE_ADMIN =
      SetAccountRoleRequestRole._(
          2, _omitEnumNames ? '' : 'SET_ACCOUNT_ROLE_REQUEST_ROLE_ADMIN');
  static const SetAccountRoleRequestRole SET_ACCOUNT_ROLE_REQUEST_ROLE_MEMBER =
      SetAccountRoleRequestRole._(
          3, _omitEnumNames ? '' : 'SET_ACCOUNT_ROLE_REQUEST_ROLE_MEMBER');
  static const SetAccountRoleRequestRole SET_ACCOUNT_ROLE_REQUEST_ROLE_GUEST =
      SetAccountRoleRequestRole._(
          4, _omitEnumNames ? '' : 'SET_ACCOUNT_ROLE_REQUEST_ROLE_GUEST');

  static const $core.List<SetAccountRoleRequestRole> values =
      <SetAccountRoleRequestRole>[
    SET_ACCOUNT_ROLE_REQUEST_ROLE_UNSPECIFIED,
    SET_ACCOUNT_ROLE_REQUEST_ROLE_OWNER,
    SET_ACCOUNT_ROLE_REQUEST_ROLE_ADMIN,
    SET_ACCOUNT_ROLE_REQUEST_ROLE_MEMBER,
    SET_ACCOUNT_ROLE_REQUEST_ROLE_GUEST,
  ];

  static final $core.List<SetAccountRoleRequestRole?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static SetAccountRoleRequestRole? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SetAccountRoleRequestRole._(super.value, super.name);
}

class CreateGroupRequestColor extends $pb.ProtobufEnum {
  static const CreateGroupRequestColor CREATE_GROUP_REQUEST_COLOR_UNSPECIFIED =
      CreateGroupRequestColor._(
          0, _omitEnumNames ? '' : 'CREATE_GROUP_REQUEST_COLOR_UNSPECIFIED');
  static const CreateGroupRequestColor CREATE_GROUP_REQUEST_COLOR_LAVENDER =
      CreateGroupRequestColor._(
          1, _omitEnumNames ? '' : 'CREATE_GROUP_REQUEST_COLOR_LAVENDER');
  static const CreateGroupRequestColor CREATE_GROUP_REQUEST_COLOR_CYAN =
      CreateGroupRequestColor._(
          2, _omitEnumNames ? '' : 'CREATE_GROUP_REQUEST_COLOR_CYAN');
  static const CreateGroupRequestColor CREATE_GROUP_REQUEST_COLOR_PINK =
      CreateGroupRequestColor._(
          3, _omitEnumNames ? '' : 'CREATE_GROUP_REQUEST_COLOR_PINK');
  static const CreateGroupRequestColor CREATE_GROUP_REQUEST_COLOR_YELLOW =
      CreateGroupRequestColor._(
          4, _omitEnumNames ? '' : 'CREATE_GROUP_REQUEST_COLOR_YELLOW');
  static const CreateGroupRequestColor CREATE_GROUP_REQUEST_COLOR_GREEN =
      CreateGroupRequestColor._(
          5, _omitEnumNames ? '' : 'CREATE_GROUP_REQUEST_COLOR_GREEN');
  static const CreateGroupRequestColor CREATE_GROUP_REQUEST_COLOR_RED =
      CreateGroupRequestColor._(
          6, _omitEnumNames ? '' : 'CREATE_GROUP_REQUEST_COLOR_RED');
  static const CreateGroupRequestColor CREATE_GROUP_REQUEST_COLOR_GREY =
      CreateGroupRequestColor._(
          7, _omitEnumNames ? '' : 'CREATE_GROUP_REQUEST_COLOR_GREY');

  static const $core.List<CreateGroupRequestColor> values =
      <CreateGroupRequestColor>[
    CREATE_GROUP_REQUEST_COLOR_UNSPECIFIED,
    CREATE_GROUP_REQUEST_COLOR_LAVENDER,
    CREATE_GROUP_REQUEST_COLOR_CYAN,
    CREATE_GROUP_REQUEST_COLOR_PINK,
    CREATE_GROUP_REQUEST_COLOR_YELLOW,
    CREATE_GROUP_REQUEST_COLOR_GREEN,
    CREATE_GROUP_REQUEST_COLOR_RED,
    CREATE_GROUP_REQUEST_COLOR_GREY,
  ];

  static final $core.List<CreateGroupRequestColor?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static CreateGroupRequestColor? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CreateGroupRequestColor._(super.value, super.name);
}

class UpdateGroupRequestColor extends $pb.ProtobufEnum {
  static const UpdateGroupRequestColor UPDATE_GROUP_REQUEST_COLOR_UNSPECIFIED =
      UpdateGroupRequestColor._(
          0, _omitEnumNames ? '' : 'UPDATE_GROUP_REQUEST_COLOR_UNSPECIFIED');
  static const UpdateGroupRequestColor UPDATE_GROUP_REQUEST_COLOR_LAVENDER =
      UpdateGroupRequestColor._(
          1, _omitEnumNames ? '' : 'UPDATE_GROUP_REQUEST_COLOR_LAVENDER');
  static const UpdateGroupRequestColor UPDATE_GROUP_REQUEST_COLOR_CYAN =
      UpdateGroupRequestColor._(
          2, _omitEnumNames ? '' : 'UPDATE_GROUP_REQUEST_COLOR_CYAN');
  static const UpdateGroupRequestColor UPDATE_GROUP_REQUEST_COLOR_PINK =
      UpdateGroupRequestColor._(
          3, _omitEnumNames ? '' : 'UPDATE_GROUP_REQUEST_COLOR_PINK');
  static const UpdateGroupRequestColor UPDATE_GROUP_REQUEST_COLOR_YELLOW =
      UpdateGroupRequestColor._(
          4, _omitEnumNames ? '' : 'UPDATE_GROUP_REQUEST_COLOR_YELLOW');
  static const UpdateGroupRequestColor UPDATE_GROUP_REQUEST_COLOR_GREEN =
      UpdateGroupRequestColor._(
          5, _omitEnumNames ? '' : 'UPDATE_GROUP_REQUEST_COLOR_GREEN');
  static const UpdateGroupRequestColor UPDATE_GROUP_REQUEST_COLOR_RED =
      UpdateGroupRequestColor._(
          6, _omitEnumNames ? '' : 'UPDATE_GROUP_REQUEST_COLOR_RED');
  static const UpdateGroupRequestColor UPDATE_GROUP_REQUEST_COLOR_GREY =
      UpdateGroupRequestColor._(
          7, _omitEnumNames ? '' : 'UPDATE_GROUP_REQUEST_COLOR_GREY');

  static const $core.List<UpdateGroupRequestColor> values =
      <UpdateGroupRequestColor>[
    UPDATE_GROUP_REQUEST_COLOR_UNSPECIFIED,
    UPDATE_GROUP_REQUEST_COLOR_LAVENDER,
    UPDATE_GROUP_REQUEST_COLOR_CYAN,
    UPDATE_GROUP_REQUEST_COLOR_PINK,
    UPDATE_GROUP_REQUEST_COLOR_YELLOW,
    UPDATE_GROUP_REQUEST_COLOR_GREEN,
    UPDATE_GROUP_REQUEST_COLOR_RED,
    UPDATE_GROUP_REQUEST_COLOR_GREY,
  ];

  static final $core.List<UpdateGroupRequestColor?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static UpdateGroupRequestColor? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdateGroupRequestColor._(super.value, super.name);
}

class WordFilterAction extends $pb.ProtobufEnum {
  static const WordFilterAction WORD_FILTER_ACTION_UNSPECIFIED =
      WordFilterAction._(
          0, _omitEnumNames ? '' : 'WORD_FILTER_ACTION_UNSPECIFIED');
  static const WordFilterAction WORD_FILTER_ACTION_BLOCK =
      WordFilterAction._(1, _omitEnumNames ? '' : 'WORD_FILTER_ACTION_BLOCK');
  static const WordFilterAction WORD_FILTER_ACTION_FLAG =
      WordFilterAction._(2, _omitEnumNames ? '' : 'WORD_FILTER_ACTION_FLAG');

  static const $core.List<WordFilterAction> values = <WordFilterAction>[
    WORD_FILTER_ACTION_UNSPECIFIED,
    WORD_FILTER_ACTION_BLOCK,
    WORD_FILTER_ACTION_FLAG,
  ];

  static final $core.List<WordFilterAction?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static WordFilterAction? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const WordFilterAction._(super.value, super.name);
}

class CreateWordFilterRequestAction extends $pb.ProtobufEnum {
  static const CreateWordFilterRequestAction
      CREATE_WORD_FILTER_REQUEST_ACTION_UNSPECIFIED =
      CreateWordFilterRequestAction._(
          0,
          _omitEnumNames
              ? ''
              : 'CREATE_WORD_FILTER_REQUEST_ACTION_UNSPECIFIED');
  static const CreateWordFilterRequestAction
      CREATE_WORD_FILTER_REQUEST_ACTION_BLOCK = CreateWordFilterRequestAction._(
          1, _omitEnumNames ? '' : 'CREATE_WORD_FILTER_REQUEST_ACTION_BLOCK');
  static const CreateWordFilterRequestAction
      CREATE_WORD_FILTER_REQUEST_ACTION_FLAG = CreateWordFilterRequestAction._(
          2, _omitEnumNames ? '' : 'CREATE_WORD_FILTER_REQUEST_ACTION_FLAG');

  static const $core.List<CreateWordFilterRequestAction> values =
      <CreateWordFilterRequestAction>[
    CREATE_WORD_FILTER_REQUEST_ACTION_UNSPECIFIED,
    CREATE_WORD_FILTER_REQUEST_ACTION_BLOCK,
    CREATE_WORD_FILTER_REQUEST_ACTION_FLAG,
  ];

  static final $core.List<CreateWordFilterRequestAction?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static CreateWordFilterRequestAction? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CreateWordFilterRequestAction._(super.value, super.name);
}

class UpdateWordFilterRequestAction extends $pb.ProtobufEnum {
  static const UpdateWordFilterRequestAction
      UPDATE_WORD_FILTER_REQUEST_ACTION_UNSPECIFIED =
      UpdateWordFilterRequestAction._(
          0,
          _omitEnumNames
              ? ''
              : 'UPDATE_WORD_FILTER_REQUEST_ACTION_UNSPECIFIED');
  static const UpdateWordFilterRequestAction
      UPDATE_WORD_FILTER_REQUEST_ACTION_BLOCK = UpdateWordFilterRequestAction._(
          1, _omitEnumNames ? '' : 'UPDATE_WORD_FILTER_REQUEST_ACTION_BLOCK');
  static const UpdateWordFilterRequestAction
      UPDATE_WORD_FILTER_REQUEST_ACTION_FLAG = UpdateWordFilterRequestAction._(
          2, _omitEnumNames ? '' : 'UPDATE_WORD_FILTER_REQUEST_ACTION_FLAG');

  static const $core.List<UpdateWordFilterRequestAction> values =
      <UpdateWordFilterRequestAction>[
    UPDATE_WORD_FILTER_REQUEST_ACTION_UNSPECIFIED,
    UPDATE_WORD_FILTER_REQUEST_ACTION_BLOCK,
    UPDATE_WORD_FILTER_REQUEST_ACTION_FLAG,
  ];

  static final $core.List<UpdateWordFilterRequestAction?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static UpdateWordFilterRequestAction? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdateWordFilterRequestAction._(super.value, super.name);
}

class PurgeRequestOutcome extends $pb.ProtobufEnum {
  static const PurgeRequestOutcome PURGE_REQUEST_OUTCOME_UNSPECIFIED =
      PurgeRequestOutcome._(
          0, _omitEnumNames ? '' : 'PURGE_REQUEST_OUTCOME_UNSPECIFIED');
  static const PurgeRequestOutcome PURGE_REQUEST_OUTCOME_SHOWN =
      PurgeRequestOutcome._(
          1, _omitEnumNames ? '' : 'PURGE_REQUEST_OUTCOME_SHOWN');
  static const PurgeRequestOutcome PURGE_REQUEST_OUTCOME_HIDDEN =
      PurgeRequestOutcome._(
          2, _omitEnumNames ? '' : 'PURGE_REQUEST_OUTCOME_HIDDEN');
  static const PurgeRequestOutcome PURGE_REQUEST_OUTCOME_ERASED =
      PurgeRequestOutcome._(
          3, _omitEnumNames ? '' : 'PURGE_REQUEST_OUTCOME_ERASED');

  static const $core.List<PurgeRequestOutcome> values = <PurgeRequestOutcome>[
    PURGE_REQUEST_OUTCOME_UNSPECIFIED,
    PURGE_REQUEST_OUTCOME_SHOWN,
    PURGE_REQUEST_OUTCOME_HIDDEN,
    PURGE_REQUEST_OUTCOME_ERASED,
  ];

  static final $core.List<PurgeRequestOutcome?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static PurgeRequestOutcome? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PurgeRequestOutcome._(super.value, super.name);
}

class DecidePurgeRequestRequestOutcome extends $pb.ProtobufEnum {
  static const DecidePurgeRequestRequestOutcome
      DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_UNSPECIFIED =
      DecidePurgeRequestRequestOutcome._(
          0,
          _omitEnumNames
              ? ''
              : 'DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_UNSPECIFIED');
  static const DecidePurgeRequestRequestOutcome
      DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_SHOWN =
      DecidePurgeRequestRequestOutcome._(1,
          _omitEnumNames ? '' : 'DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_SHOWN');
  static const DecidePurgeRequestRequestOutcome
      DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_HIDDEN =
      DecidePurgeRequestRequestOutcome._(2,
          _omitEnumNames ? '' : 'DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_HIDDEN');
  static const DecidePurgeRequestRequestOutcome
      DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_ERASED =
      DecidePurgeRequestRequestOutcome._(3,
          _omitEnumNames ? '' : 'DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_ERASED');

  static const $core.List<DecidePurgeRequestRequestOutcome> values =
      <DecidePurgeRequestRequestOutcome>[
    DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_UNSPECIFIED,
    DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_SHOWN,
    DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_HIDDEN,
    DECIDE_PURGE_REQUEST_REQUEST_OUTCOME_ERASED,
  ];

  static final $core.List<DecidePurgeRequestRequestOutcome?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static DecidePurgeRequestRequestOutcome? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const DecidePurgeRequestRequestOutcome._(super.value, super.name);
}

class IntegrationAllows extends $pb.ProtobufEnum {
  static const IntegrationAllows INTEGRATION_ALLOWS_UNSPECIFIED =
      IntegrationAllows._(
          0, _omitEnumNames ? '' : 'INTEGRATION_ALLOWS_UNSPECIFIED');
  static const IntegrationAllows INTEGRATION_ALLOWS_AVAILABILITY =
      IntegrationAllows._(
          1, _omitEnumNames ? '' : 'INTEGRATION_ALLOWS_AVAILABILITY');

  static const $core.List<IntegrationAllows> values = <IntegrationAllows>[
    INTEGRATION_ALLOWS_UNSPECIFIED,
    INTEGRATION_ALLOWS_AVAILABILITY,
  ];

  static final $core.List<IntegrationAllows?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 1);
  static IntegrationAllows? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const IntegrationAllows._(super.value, super.name);
}

class CreateIntegrationRequestAllows extends $pb.ProtobufEnum {
  static const CreateIntegrationRequestAllows
      CREATE_INTEGRATION_REQUEST_ALLOWS_UNSPECIFIED =
      CreateIntegrationRequestAllows._(
          0,
          _omitEnumNames
              ? ''
              : 'CREATE_INTEGRATION_REQUEST_ALLOWS_UNSPECIFIED');
  static const CreateIntegrationRequestAllows
      CREATE_INTEGRATION_REQUEST_ALLOWS_AVAILABILITY =
      CreateIntegrationRequestAllows._(
          1,
          _omitEnumNames
              ? ''
              : 'CREATE_INTEGRATION_REQUEST_ALLOWS_AVAILABILITY');

  static const $core.List<CreateIntegrationRequestAllows> values =
      <CreateIntegrationRequestAllows>[
    CREATE_INTEGRATION_REQUEST_ALLOWS_UNSPECIFIED,
    CREATE_INTEGRATION_REQUEST_ALLOWS_AVAILABILITY,
  ];

  static final $core.List<CreateIntegrationRequestAllows?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 1);
  static CreateIntegrationRequestAllows? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CreateIntegrationRequestAllows._(super.value, super.name);
}

class PeerKind extends $pb.ProtobufEnum {
  static const PeerKind PEER_KIND_UNSPECIFIED =
      PeerKind._(0, _omitEnumNames ? '' : 'PEER_KIND_UNSPECIFIED');
  static const PeerKind PEER_KIND_SERVER =
      PeerKind._(1, _omitEnumNames ? '' : 'PEER_KIND_SERVER');
  static const PeerKind PEER_KIND_HUB =
      PeerKind._(2, _omitEnumNames ? '' : 'PEER_KIND_HUB');

  static const $core.List<PeerKind> values = <PeerKind>[
    PEER_KIND_UNSPECIFIED,
    PEER_KIND_SERVER,
    PEER_KIND_HUB,
  ];

  static final $core.List<PeerKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static PeerKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PeerKind._(super.value, super.name);
}

class PeerState extends $pb.ProtobufEnum {
  static const PeerState PEER_STATE_UNSPECIFIED =
      PeerState._(0, _omitEnumNames ? '' : 'PEER_STATE_UNSPECIFIED');
  static const PeerState PEER_STATE_INVITED =
      PeerState._(1, _omitEnumNames ? '' : 'PEER_STATE_INVITED');
  static const PeerState PEER_STATE_ACTIVE =
      PeerState._(2, _omitEnumNames ? '' : 'PEER_STATE_ACTIVE');
  static const PeerState PEER_STATE_PAUSED =
      PeerState._(3, _omitEnumNames ? '' : 'PEER_STATE_PAUSED');
  static const PeerState PEER_STATE_REMOVED =
      PeerState._(4, _omitEnumNames ? '' : 'PEER_STATE_REMOVED');

  static const $core.List<PeerState> values = <PeerState>[
    PEER_STATE_UNSPECIFIED,
    PEER_STATE_INVITED,
    PEER_STATE_ACTIVE,
    PEER_STATE_PAUSED,
    PEER_STATE_REMOVED,
  ];

  static final $core.List<PeerState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static PeerState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PeerState._(super.value, super.name);
}

class UpdatePeerRequestState extends $pb.ProtobufEnum {
  static const UpdatePeerRequestState UPDATE_PEER_REQUEST_STATE_UNSPECIFIED =
      UpdatePeerRequestState._(
          0, _omitEnumNames ? '' : 'UPDATE_PEER_REQUEST_STATE_UNSPECIFIED');
  static const UpdatePeerRequestState UPDATE_PEER_REQUEST_STATE_ACTIVE =
      UpdatePeerRequestState._(
          1, _omitEnumNames ? '' : 'UPDATE_PEER_REQUEST_STATE_ACTIVE');
  static const UpdatePeerRequestState UPDATE_PEER_REQUEST_STATE_PAUSED =
      UpdatePeerRequestState._(
          2, _omitEnumNames ? '' : 'UPDATE_PEER_REQUEST_STATE_PAUSED');

  static const $core.List<UpdatePeerRequestState> values =
      <UpdatePeerRequestState>[
    UPDATE_PEER_REQUEST_STATE_UNSPECIFIED,
    UPDATE_PEER_REQUEST_STATE_ACTIVE,
    UPDATE_PEER_REQUEST_STATE_PAUSED,
  ];

  static final $core.List<UpdatePeerRequestState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static UpdatePeerRequestState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const UpdatePeerRequestState._(super.value, super.name);
}

class StreamReference extends $pb.ProtobufEnum {
  static const StreamReference STREAM_REFERENCE_UNSPECIFIED = StreamReference._(
      0, _omitEnumNames ? '' : 'STREAM_REFERENCE_UNSPECIFIED');
  static const StreamReference STREAM_REFERENCE_SELF =
      StreamReference._(1, _omitEnumNames ? '' : 'STREAM_REFERENCE_SELF');
  static const StreamReference STREAM_REFERENCE_OWNER =
      StreamReference._(2, _omitEnumNames ? '' : 'STREAM_REFERENCE_OWNER');
  static const StreamReference STREAM_REFERENCE_HUB =
      StreamReference._(3, _omitEnumNames ? '' : 'STREAM_REFERENCE_HUB');

  static const $core.List<StreamReference> values = <StreamReference>[
    STREAM_REFERENCE_UNSPECIFIED,
    STREAM_REFERENCE_SELF,
    STREAM_REFERENCE_OWNER,
    STREAM_REFERENCE_HUB,
  ];

  static final $core.List<StreamReference?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static StreamReference? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const StreamReference._(super.value, super.name);
}

class StreamKind extends $pb.ProtobufEnum {
  static const StreamKind STREAM_KIND_UNSPECIFIED =
      StreamKind._(0, _omitEnumNames ? '' : 'STREAM_KIND_UNSPECIFIED');
  static const StreamKind STREAM_KIND_CHANNEL =
      StreamKind._(1, _omitEnumNames ? '' : 'STREAM_KIND_CHANNEL');
  static const StreamKind STREAM_KIND_ADHOC =
      StreamKind._(2, _omitEnumNames ? '' : 'STREAM_KIND_ADHOC');
  static const StreamKind STREAM_KIND_SEALED =
      StreamKind._(3, _omitEnumNames ? '' : 'STREAM_KIND_SEALED');

  static const $core.List<StreamKind> values = <StreamKind>[
    STREAM_KIND_UNSPECIFIED,
    STREAM_KIND_CHANNEL,
    STREAM_KIND_ADHOC,
    STREAM_KIND_SEALED,
  ];

  static final $core.List<StreamKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static StreamKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const StreamKind._(super.value, super.name);
}

class StreamState extends $pb.ProtobufEnum {
  static const StreamState STREAM_STATE_UNSPECIFIED =
      StreamState._(0, _omitEnumNames ? '' : 'STREAM_STATE_UNSPECIFIED');
  static const StreamState STREAM_STATE_OFFERED =
      StreamState._(1, _omitEnumNames ? '' : 'STREAM_STATE_OFFERED');
  static const StreamState STREAM_STATE_ACTIVE =
      StreamState._(2, _omitEnumNames ? '' : 'STREAM_STATE_ACTIVE');
  static const StreamState STREAM_STATE_LEFT =
      StreamState._(3, _omitEnumNames ? '' : 'STREAM_STATE_LEFT');
  static const StreamState STREAM_STATE_REMOVED =
      StreamState._(4, _omitEnumNames ? '' : 'STREAM_STATE_REMOVED');

  static const $core.List<StreamState> values = <StreamState>[
    STREAM_STATE_UNSPECIFIED,
    STREAM_STATE_OFFERED,
    STREAM_STATE_ACTIVE,
    STREAM_STATE_LEFT,
    STREAM_STATE_REMOVED,
  ];

  static final $core.List<StreamState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static StreamState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const StreamState._(super.value, super.name);
}

class StreamServerState extends $pb.ProtobufEnum {
  static const StreamServerState STREAM_SERVER_STATE_UNSPECIFIED =
      StreamServerState._(
          0, _omitEnumNames ? '' : 'STREAM_SERVER_STATE_UNSPECIFIED');
  static const StreamServerState STREAM_SERVER_STATE_INVITED =
      StreamServerState._(
          1, _omitEnumNames ? '' : 'STREAM_SERVER_STATE_INVITED');
  static const StreamServerState STREAM_SERVER_STATE_ACTIVE =
      StreamServerState._(
          2, _omitEnumNames ? '' : 'STREAM_SERVER_STATE_ACTIVE');
  static const StreamServerState STREAM_SERVER_STATE_REMOVED =
      StreamServerState._(
          3, _omitEnumNames ? '' : 'STREAM_SERVER_STATE_REMOVED');

  static const $core.List<StreamServerState> values = <StreamServerState>[
    STREAM_SERVER_STATE_UNSPECIFIED,
    STREAM_SERVER_STATE_INVITED,
    STREAM_SERVER_STATE_ACTIVE,
    STREAM_SERVER_STATE_REMOVED,
  ];

  static final $core.List<StreamServerState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static StreamServerState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const StreamServerState._(super.value, super.name);
}

class ShareChannelRequestListing extends $pb.ProtobufEnum {
  static const ShareChannelRequestListing
      SHARE_CHANNEL_REQUEST_LISTING_UNSPECIFIED = ShareChannelRequestListing._(
          0, _omitEnumNames ? '' : 'SHARE_CHANNEL_REQUEST_LISTING_UNSPECIFIED');
  static const ShareChannelRequestListing
      SHARE_CHANNEL_REQUEST_LISTING_UNLISTED = ShareChannelRequestListing._(
          1, _omitEnumNames ? '' : 'SHARE_CHANNEL_REQUEST_LISTING_UNLISTED');
  static const ShareChannelRequestListing
      SHARE_CHANNEL_REQUEST_LISTING_REQUEST = ShareChannelRequestListing._(
          2, _omitEnumNames ? '' : 'SHARE_CHANNEL_REQUEST_LISTING_REQUEST');
  static const ShareChannelRequestListing SHARE_CHANNEL_REQUEST_LISTING_OPEN =
      ShareChannelRequestListing._(
          3, _omitEnumNames ? '' : 'SHARE_CHANNEL_REQUEST_LISTING_OPEN');

  static const $core.List<ShareChannelRequestListing> values =
      <ShareChannelRequestListing>[
    SHARE_CHANNEL_REQUEST_LISTING_UNSPECIFIED,
    SHARE_CHANNEL_REQUEST_LISTING_UNLISTED,
    SHARE_CHANNEL_REQUEST_LISTING_REQUEST,
    SHARE_CHANNEL_REQUEST_LISTING_OPEN,
  ];

  static final $core.List<ShareChannelRequestListing?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ShareChannelRequestListing? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ShareChannelRequestListing._(super.value, super.name);
}

class InspectPeerInvitationResponseKind extends $pb.ProtobufEnum {
  static const InspectPeerInvitationResponseKind
      INSPECT_PEER_INVITATION_RESPONSE_KIND_UNSPECIFIED =
      InspectPeerInvitationResponseKind._(
          0,
          _omitEnumNames
              ? ''
              : 'INSPECT_PEER_INVITATION_RESPONSE_KIND_UNSPECIFIED');
  static const InspectPeerInvitationResponseKind
      INSPECT_PEER_INVITATION_RESPONSE_KIND_SERVER =
      InspectPeerInvitationResponseKind._(1,
          _omitEnumNames ? '' : 'INSPECT_PEER_INVITATION_RESPONSE_KIND_SERVER');
  static const InspectPeerInvitationResponseKind
      INSPECT_PEER_INVITATION_RESPONSE_KIND_HUB =
      InspectPeerInvitationResponseKind._(
          2, _omitEnumNames ? '' : 'INSPECT_PEER_INVITATION_RESPONSE_KIND_HUB');

  static const $core.List<InspectPeerInvitationResponseKind> values =
      <InspectPeerInvitationResponseKind>[
    INSPECT_PEER_INVITATION_RESPONSE_KIND_UNSPECIFIED,
    INSPECT_PEER_INVITATION_RESPONSE_KIND_SERVER,
    INSPECT_PEER_INVITATION_RESPONSE_KIND_HUB,
  ];

  static final $core.List<InspectPeerInvitationResponseKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static InspectPeerInvitationResponseKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const InspectPeerInvitationResponseKind._(super.value, super.name);
}

class HubDirectoryEntryListing extends $pb.ProtobufEnum {
  static const HubDirectoryEntryListing
      HUB_DIRECTORY_ENTRY_LISTING_UNSPECIFIED = HubDirectoryEntryListing._(
          0, _omitEnumNames ? '' : 'HUB_DIRECTORY_ENTRY_LISTING_UNSPECIFIED');
  static const HubDirectoryEntryListing HUB_DIRECTORY_ENTRY_LISTING_REQUEST =
      HubDirectoryEntryListing._(
          1, _omitEnumNames ? '' : 'HUB_DIRECTORY_ENTRY_LISTING_REQUEST');
  static const HubDirectoryEntryListing HUB_DIRECTORY_ENTRY_LISTING_OPEN =
      HubDirectoryEntryListing._(
          2, _omitEnumNames ? '' : 'HUB_DIRECTORY_ENTRY_LISTING_OPEN');

  static const $core.List<HubDirectoryEntryListing> values =
      <HubDirectoryEntryListing>[
    HUB_DIRECTORY_ENTRY_LISTING_UNSPECIFIED,
    HUB_DIRECTORY_ENTRY_LISTING_REQUEST,
    HUB_DIRECTORY_ENTRY_LISTING_OPEN,
  ];

  static final $core.List<HubDirectoryEntryListing?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static HubDirectoryEntryListing? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const HubDirectoryEntryListing._(super.value, super.name);
}

class HubDirectoryEntryState extends $pb.ProtobufEnum {
  static const HubDirectoryEntryState HUB_DIRECTORY_ENTRY_STATE_UNSPECIFIED =
      HubDirectoryEntryState._(
          0, _omitEnumNames ? '' : 'HUB_DIRECTORY_ENTRY_STATE_UNSPECIFIED');
  static const HubDirectoryEntryState HUB_DIRECTORY_ENTRY_STATE_NONE =
      HubDirectoryEntryState._(
          1, _omitEnumNames ? '' : 'HUB_DIRECTORY_ENTRY_STATE_NONE');
  static const HubDirectoryEntryState HUB_DIRECTORY_ENTRY_STATE_INVITED =
      HubDirectoryEntryState._(
          2, _omitEnumNames ? '' : 'HUB_DIRECTORY_ENTRY_STATE_INVITED');
  static const HubDirectoryEntryState HUB_DIRECTORY_ENTRY_STATE_REQUESTED =
      HubDirectoryEntryState._(
          3, _omitEnumNames ? '' : 'HUB_DIRECTORY_ENTRY_STATE_REQUESTED');
  static const HubDirectoryEntryState HUB_DIRECTORY_ENTRY_STATE_MEMBER =
      HubDirectoryEntryState._(
          4, _omitEnumNames ? '' : 'HUB_DIRECTORY_ENTRY_STATE_MEMBER');

  static const $core.List<HubDirectoryEntryState> values =
      <HubDirectoryEntryState>[
    HUB_DIRECTORY_ENTRY_STATE_UNSPECIFIED,
    HUB_DIRECTORY_ENTRY_STATE_NONE,
    HUB_DIRECTORY_ENTRY_STATE_INVITED,
    HUB_DIRECTORY_ENTRY_STATE_REQUESTED,
    HUB_DIRECTORY_ENTRY_STATE_MEMBER,
  ];

  static final $core.List<HubDirectoryEntryState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static HubDirectoryEntryState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const HubDirectoryEntryState._(super.value, super.name);
}

class StreamMoveDirection extends $pb.ProtobufEnum {
  static const StreamMoveDirection STREAM_MOVE_DIRECTION_UNSPECIFIED =
      StreamMoveDirection._(
          0, _omitEnumNames ? '' : 'STREAM_MOVE_DIRECTION_UNSPECIFIED');
  static const StreamMoveDirection STREAM_MOVE_DIRECTION_OUTGOING =
      StreamMoveDirection._(
          1, _omitEnumNames ? '' : 'STREAM_MOVE_DIRECTION_OUTGOING');
  static const StreamMoveDirection STREAM_MOVE_DIRECTION_INCOMING =
      StreamMoveDirection._(
          2, _omitEnumNames ? '' : 'STREAM_MOVE_DIRECTION_INCOMING');

  static const $core.List<StreamMoveDirection> values = <StreamMoveDirection>[
    STREAM_MOVE_DIRECTION_UNSPECIFIED,
    STREAM_MOVE_DIRECTION_OUTGOING,
    STREAM_MOVE_DIRECTION_INCOMING,
  ];

  static final $core.List<StreamMoveDirection?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static StreamMoveDirection? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const StreamMoveDirection._(super.value, super.name);
}

class StreamMoveState extends $pb.ProtobufEnum {
  static const StreamMoveState STREAM_MOVE_STATE_UNSPECIFIED =
      StreamMoveState._(
          0, _omitEnumNames ? '' : 'STREAM_MOVE_STATE_UNSPECIFIED');
  static const StreamMoveState STREAM_MOVE_STATE_PROPOSED =
      StreamMoveState._(1, _omitEnumNames ? '' : 'STREAM_MOVE_STATE_PROPOSED');
  static const StreamMoveState STREAM_MOVE_STATE_ACCEPTED =
      StreamMoveState._(2, _omitEnumNames ? '' : 'STREAM_MOVE_STATE_ACCEPTED');
  static const StreamMoveState STREAM_MOVE_STATE_COMMITTED =
      StreamMoveState._(3, _omitEnumNames ? '' : 'STREAM_MOVE_STATE_COMMITTED');
  static const StreamMoveState STREAM_MOVE_STATE_DONE =
      StreamMoveState._(4, _omitEnumNames ? '' : 'STREAM_MOVE_STATE_DONE');
  static const StreamMoveState STREAM_MOVE_STATE_DECLINED =
      StreamMoveState._(5, _omitEnumNames ? '' : 'STREAM_MOVE_STATE_DECLINED');
  static const StreamMoveState STREAM_MOVE_STATE_ABANDONED =
      StreamMoveState._(6, _omitEnumNames ? '' : 'STREAM_MOVE_STATE_ABANDONED');

  static const $core.List<StreamMoveState> values = <StreamMoveState>[
    STREAM_MOVE_STATE_UNSPECIFIED,
    STREAM_MOVE_STATE_PROPOSED,
    STREAM_MOVE_STATE_ACCEPTED,
    STREAM_MOVE_STATE_COMMITTED,
    STREAM_MOVE_STATE_DONE,
    STREAM_MOVE_STATE_DECLINED,
    STREAM_MOVE_STATE_ABANDONED,
  ];

  static final $core.List<StreamMoveState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 6);
  static StreamMoveState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const StreamMoveState._(super.value, super.name);
}

class HubChannelListing extends $pb.ProtobufEnum {
  static const HubChannelListing HUB_CHANNEL_LISTING_UNSPECIFIED =
      HubChannelListing._(
          0, _omitEnumNames ? '' : 'HUB_CHANNEL_LISTING_UNSPECIFIED');
  static const HubChannelListing HUB_CHANNEL_LISTING_UNLISTED =
      HubChannelListing._(
          1, _omitEnumNames ? '' : 'HUB_CHANNEL_LISTING_UNLISTED');
  static const HubChannelListing HUB_CHANNEL_LISTING_REQUEST =
      HubChannelListing._(
          2, _omitEnumNames ? '' : 'HUB_CHANNEL_LISTING_REQUEST');
  static const HubChannelListing HUB_CHANNEL_LISTING_OPEN =
      HubChannelListing._(3, _omitEnumNames ? '' : 'HUB_CHANNEL_LISTING_OPEN');

  static const $core.List<HubChannelListing> values = <HubChannelListing>[
    HUB_CHANNEL_LISTING_UNSPECIFIED,
    HUB_CHANNEL_LISTING_UNLISTED,
    HUB_CHANNEL_LISTING_REQUEST,
    HUB_CHANNEL_LISTING_OPEN,
  ];

  static final $core.List<HubChannelListing?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static HubChannelListing? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const HubChannelListing._(super.value, super.name);
}

class ModerateHubChannelRequestAction extends $pb.ProtobufEnum {
  static const ModerateHubChannelRequestAction
      MODERATE_HUB_CHANNEL_REQUEST_ACTION_UNSPECIFIED =
      ModerateHubChannelRequestAction._(
          0,
          _omitEnumNames
              ? ''
              : 'MODERATE_HUB_CHANNEL_REQUEST_ACTION_UNSPECIFIED');
  static const ModerateHubChannelRequestAction
      MODERATE_HUB_CHANNEL_REQUEST_ACTION_INVITE =
      ModerateHubChannelRequestAction._(1,
          _omitEnumNames ? '' : 'MODERATE_HUB_CHANNEL_REQUEST_ACTION_INVITE');
  static const ModerateHubChannelRequestAction
      MODERATE_HUB_CHANNEL_REQUEST_ACTION_APPROVE =
      ModerateHubChannelRequestAction._(2,
          _omitEnumNames ? '' : 'MODERATE_HUB_CHANNEL_REQUEST_ACTION_APPROVE');
  static const ModerateHubChannelRequestAction
      MODERATE_HUB_CHANNEL_REQUEST_ACTION_DECLINE =
      ModerateHubChannelRequestAction._(3,
          _omitEnumNames ? '' : 'MODERATE_HUB_CHANNEL_REQUEST_ACTION_DECLINE');
  static const ModerateHubChannelRequestAction
      MODERATE_HUB_CHANNEL_REQUEST_ACTION_REMOVE_SERVER =
      ModerateHubChannelRequestAction._(
          4,
          _omitEnumNames
              ? ''
              : 'MODERATE_HUB_CHANNEL_REQUEST_ACTION_REMOVE_SERVER');
  static const ModerateHubChannelRequestAction
      MODERATE_HUB_CHANNEL_REQUEST_ACTION_ADD_MODERATOR =
      ModerateHubChannelRequestAction._(
          5,
          _omitEnumNames
              ? ''
              : 'MODERATE_HUB_CHANNEL_REQUEST_ACTION_ADD_MODERATOR');
  static const ModerateHubChannelRequestAction
      MODERATE_HUB_CHANNEL_REQUEST_ACTION_REMOVE_MODERATOR =
      ModerateHubChannelRequestAction._(
          6,
          _omitEnumNames
              ? ''
              : 'MODERATE_HUB_CHANNEL_REQUEST_ACTION_REMOVE_MODERATOR');
  static const ModerateHubChannelRequestAction
      MODERATE_HUB_CHANNEL_REQUEST_ACTION_DESCRIBE =
      ModerateHubChannelRequestAction._(7,
          _omitEnumNames ? '' : 'MODERATE_HUB_CHANNEL_REQUEST_ACTION_DESCRIBE');
  static const ModerateHubChannelRequestAction
      MODERATE_HUB_CHANNEL_REQUEST_ACTION_SET_LISTING =
      ModerateHubChannelRequestAction._(
          8,
          _omitEnumNames
              ? ''
              : 'MODERATE_HUB_CHANNEL_REQUEST_ACTION_SET_LISTING');
  static const ModerateHubChannelRequestAction
      MODERATE_HUB_CHANNEL_REQUEST_ACTION_CLOSE =
      ModerateHubChannelRequestAction._(
          9, _omitEnumNames ? '' : 'MODERATE_HUB_CHANNEL_REQUEST_ACTION_CLOSE');

  static const $core.List<ModerateHubChannelRequestAction> values =
      <ModerateHubChannelRequestAction>[
    MODERATE_HUB_CHANNEL_REQUEST_ACTION_UNSPECIFIED,
    MODERATE_HUB_CHANNEL_REQUEST_ACTION_INVITE,
    MODERATE_HUB_CHANNEL_REQUEST_ACTION_APPROVE,
    MODERATE_HUB_CHANNEL_REQUEST_ACTION_DECLINE,
    MODERATE_HUB_CHANNEL_REQUEST_ACTION_REMOVE_SERVER,
    MODERATE_HUB_CHANNEL_REQUEST_ACTION_ADD_MODERATOR,
    MODERATE_HUB_CHANNEL_REQUEST_ACTION_REMOVE_MODERATOR,
    MODERATE_HUB_CHANNEL_REQUEST_ACTION_DESCRIBE,
    MODERATE_HUB_CHANNEL_REQUEST_ACTION_SET_LISTING,
    MODERATE_HUB_CHANNEL_REQUEST_ACTION_CLOSE,
  ];

  static final $core.List<ModerateHubChannelRequestAction?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 9);
  static ModerateHubChannelRequestAction? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ModerateHubChannelRequestAction._(super.value, super.name);
}

class ModerateHubChannelRequestListing extends $pb.ProtobufEnum {
  static const ModerateHubChannelRequestListing
      MODERATE_HUB_CHANNEL_REQUEST_LISTING_UNSPECIFIED =
      ModerateHubChannelRequestListing._(
          0,
          _omitEnumNames
              ? ''
              : 'MODERATE_HUB_CHANNEL_REQUEST_LISTING_UNSPECIFIED');
  static const ModerateHubChannelRequestListing
      MODERATE_HUB_CHANNEL_REQUEST_LISTING_UNLISTED =
      ModerateHubChannelRequestListing._(
          1,
          _omitEnumNames
              ? ''
              : 'MODERATE_HUB_CHANNEL_REQUEST_LISTING_UNLISTED');
  static const ModerateHubChannelRequestListing
      MODERATE_HUB_CHANNEL_REQUEST_LISTING_REQUEST =
      ModerateHubChannelRequestListing._(2,
          _omitEnumNames ? '' : 'MODERATE_HUB_CHANNEL_REQUEST_LISTING_REQUEST');
  static const ModerateHubChannelRequestListing
      MODERATE_HUB_CHANNEL_REQUEST_LISTING_OPEN =
      ModerateHubChannelRequestListing._(
          3, _omitEnumNames ? '' : 'MODERATE_HUB_CHANNEL_REQUEST_LISTING_OPEN');

  static const $core.List<ModerateHubChannelRequestListing> values =
      <ModerateHubChannelRequestListing>[
    MODERATE_HUB_CHANNEL_REQUEST_LISTING_UNSPECIFIED,
    MODERATE_HUB_CHANNEL_REQUEST_LISTING_UNLISTED,
    MODERATE_HUB_CHANNEL_REQUEST_LISTING_REQUEST,
    MODERATE_HUB_CHANNEL_REQUEST_LISTING_OPEN,
  ];

  static final $core.List<ModerateHubChannelRequestListing?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ModerateHubChannelRequestListing? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ModerateHubChannelRequestListing._(super.value, super.name);
}

class AuditEntryActorKind extends $pb.ProtobufEnum {
  static const AuditEntryActorKind AUDIT_ENTRY_ACTOR_KIND_UNSPECIFIED =
      AuditEntryActorKind._(
          0, _omitEnumNames ? '' : 'AUDIT_ENTRY_ACTOR_KIND_UNSPECIFIED');
  static const AuditEntryActorKind AUDIT_ENTRY_ACTOR_KIND_ACCOUNT =
      AuditEntryActorKind._(
          1, _omitEnumNames ? '' : 'AUDIT_ENTRY_ACTOR_KIND_ACCOUNT');
  static const AuditEntryActorKind AUDIT_ENTRY_ACTOR_KIND_INTEGRATION =
      AuditEntryActorKind._(
          2, _omitEnumNames ? '' : 'AUDIT_ENTRY_ACTOR_KIND_INTEGRATION');
  static const AuditEntryActorKind AUDIT_ENTRY_ACTOR_KIND_SERVER =
      AuditEntryActorKind._(
          3, _omitEnumNames ? '' : 'AUDIT_ENTRY_ACTOR_KIND_SERVER');
  static const AuditEntryActorKind AUDIT_ENTRY_ACTOR_KIND_CLI =
      AuditEntryActorKind._(
          4, _omitEnumNames ? '' : 'AUDIT_ENTRY_ACTOR_KIND_CLI');

  static const $core.List<AuditEntryActorKind> values = <AuditEntryActorKind>[
    AUDIT_ENTRY_ACTOR_KIND_UNSPECIFIED,
    AUDIT_ENTRY_ACTOR_KIND_ACCOUNT,
    AUDIT_ENTRY_ACTOR_KIND_INTEGRATION,
    AUDIT_ENTRY_ACTOR_KIND_SERVER,
    AUDIT_ENTRY_ACTOR_KIND_CLI,
  ];

  static final $core.List<AuditEntryActorKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static AuditEntryActorKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AuditEntryActorKind._(super.value, super.name);
}

class ExportState extends $pb.ProtobufEnum {
  static const ExportState EXPORT_STATE_UNSPECIFIED =
      ExportState._(0, _omitEnumNames ? '' : 'EXPORT_STATE_UNSPECIFIED');
  static const ExportState EXPORT_STATE_RUNNING =
      ExportState._(1, _omitEnumNames ? '' : 'EXPORT_STATE_RUNNING');
  static const ExportState EXPORT_STATE_READY =
      ExportState._(2, _omitEnumNames ? '' : 'EXPORT_STATE_READY');
  static const ExportState EXPORT_STATE_FAILED =
      ExportState._(3, _omitEnumNames ? '' : 'EXPORT_STATE_FAILED');

  static const $core.List<ExportState> values = <ExportState>[
    EXPORT_STATE_UNSPECIFIED,
    EXPORT_STATE_RUNNING,
    EXPORT_STATE_READY,
    EXPORT_STATE_FAILED,
  ];

  static final $core.List<ExportState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ExportState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ExportState._(super.value, super.name);
}

class OverviewDatabaseKind extends $pb.ProtobufEnum {
  static const OverviewDatabaseKind OVERVIEW_DATABASE_KIND_UNSPECIFIED =
      OverviewDatabaseKind._(
          0, _omitEnumNames ? '' : 'OVERVIEW_DATABASE_KIND_UNSPECIFIED');
  static const OverviewDatabaseKind OVERVIEW_DATABASE_KIND_SQLITE =
      OverviewDatabaseKind._(
          1, _omitEnumNames ? '' : 'OVERVIEW_DATABASE_KIND_SQLITE');
  static const OverviewDatabaseKind OVERVIEW_DATABASE_KIND_POSTGRES =
      OverviewDatabaseKind._(
          2, _omitEnumNames ? '' : 'OVERVIEW_DATABASE_KIND_POSTGRES');

  static const $core.List<OverviewDatabaseKind> values = <OverviewDatabaseKind>[
    OVERVIEW_DATABASE_KIND_UNSPECIFIED,
    OVERVIEW_DATABASE_KIND_SQLITE,
    OVERVIEW_DATABASE_KIND_POSTGRES,
  ];

  static final $core.List<OverviewDatabaseKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static OverviewDatabaseKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const OverviewDatabaseKind._(super.value, super.name);
}

class OverviewFilesStore extends $pb.ProtobufEnum {
  static const OverviewFilesStore OVERVIEW_FILES_STORE_UNSPECIFIED =
      OverviewFilesStore._(
          0, _omitEnumNames ? '' : 'OVERVIEW_FILES_STORE_UNSPECIFIED');
  static const OverviewFilesStore OVERVIEW_FILES_STORE_DISK =
      OverviewFilesStore._(
          1, _omitEnumNames ? '' : 'OVERVIEW_FILES_STORE_DISK');
  static const OverviewFilesStore OVERVIEW_FILES_STORE_S3 =
      OverviewFilesStore._(2, _omitEnumNames ? '' : 'OVERVIEW_FILES_STORE_S3');

  static const $core.List<OverviewFilesStore> values = <OverviewFilesStore>[
    OVERVIEW_FILES_STORE_UNSPECIFIED,
    OVERVIEW_FILES_STORE_DISK,
    OVERVIEW_FILES_STORE_S3,
  ];

  static final $core.List<OverviewFilesStore?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static OverviewFilesStore? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const OverviewFilesStore._(super.value, super.name);
}

class OutsideConnectionName extends $pb.ProtobufEnum {
  static const OutsideConnectionName OUTSIDE_CONNECTION_NAME_UNSPECIFIED =
      OutsideConnectionName._(
          0, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_NAME_UNSPECIFIED');
  static const OutsideConnectionName OUTSIDE_CONNECTION_NAME_CERTIFICATE =
      OutsideConnectionName._(
          1, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_NAME_CERTIFICATE');
  static const OutsideConnectionName OUTSIDE_CONNECTION_NAME_PUSH_RELAY =
      OutsideConnectionName._(
          2, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_NAME_PUSH_RELAY');
  static const OutsideConnectionName OUTSIDE_CONNECTION_NAME_UNIFIEDPUSH =
      OutsideConnectionName._(
          3, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_NAME_UNIFIEDPUSH');
  static const OutsideConnectionName OUTSIDE_CONNECTION_NAME_LINK_PREVIEWS =
      OutsideConnectionName._(
          4, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_NAME_LINK_PREVIEWS');
  static const OutsideConnectionName OUTSIDE_CONNECTION_NAME_OBJECT_STORAGE =
      OutsideConnectionName._(
          5, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_NAME_OBJECT_STORAGE');
  static const OutsideConnectionName OUTSIDE_CONNECTION_NAME_IDENTITY_PROVIDER =
      OutsideConnectionName._(
          6, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_NAME_IDENTITY_PROVIDER');
  static const OutsideConnectionName OUTSIDE_CONNECTION_NAME_PEERS =
      OutsideConnectionName._(
          7, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_NAME_PEERS');

  static const $core.List<OutsideConnectionName> values =
      <OutsideConnectionName>[
    OUTSIDE_CONNECTION_NAME_UNSPECIFIED,
    OUTSIDE_CONNECTION_NAME_CERTIFICATE,
    OUTSIDE_CONNECTION_NAME_PUSH_RELAY,
    OUTSIDE_CONNECTION_NAME_UNIFIEDPUSH,
    OUTSIDE_CONNECTION_NAME_LINK_PREVIEWS,
    OUTSIDE_CONNECTION_NAME_OBJECT_STORAGE,
    OUTSIDE_CONNECTION_NAME_IDENTITY_PROVIDER,
    OUTSIDE_CONNECTION_NAME_PEERS,
  ];

  static final $core.List<OutsideConnectionName?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 7);
  static OutsideConnectionName? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const OutsideConnectionName._(super.value, super.name);
}

class OutsideConnectionState extends $pb.ProtobufEnum {
  static const OutsideConnectionState OUTSIDE_CONNECTION_STATE_UNSPECIFIED =
      OutsideConnectionState._(
          0, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_STATE_UNSPECIFIED');
  static const OutsideConnectionState OUTSIDE_CONNECTION_STATE_OK =
      OutsideConnectionState._(
          1, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_STATE_OK');
  static const OutsideConnectionState OUTSIDE_CONNECTION_STATE_FAILING =
      OutsideConnectionState._(
          2, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_STATE_FAILING');
  static const OutsideConnectionState OUTSIDE_CONNECTION_STATE_OFF =
      OutsideConnectionState._(
          3, _omitEnumNames ? '' : 'OUTSIDE_CONNECTION_STATE_OFF');

  static const $core.List<OutsideConnectionState> values =
      <OutsideConnectionState>[
    OUTSIDE_CONNECTION_STATE_UNSPECIFIED,
    OUTSIDE_CONNECTION_STATE_OK,
    OUTSIDE_CONNECTION_STATE_FAILING,
    OUTSIDE_CONNECTION_STATE_OFF,
  ];

  static final $core.List<OutsideConnectionState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static OutsideConnectionState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const OutsideConnectionState._(super.value, super.name);
}

class ThemeKind extends $pb.ProtobufEnum {
  static const ThemeKind THEME_KIND_UNSPECIFIED =
      ThemeKind._(0, _omitEnumNames ? '' : 'THEME_KIND_UNSPECIFIED');
  static const ThemeKind THEME_KIND_DARK =
      ThemeKind._(1, _omitEnumNames ? '' : 'THEME_KIND_DARK');
  static const ThemeKind THEME_KIND_LIGHT =
      ThemeKind._(2, _omitEnumNames ? '' : 'THEME_KIND_LIGHT');

  static const $core.List<ThemeKind> values = <ThemeKind>[
    THEME_KIND_UNSPECIFIED,
    THEME_KIND_DARK,
    THEME_KIND_LIGHT,
  ];

  static final $core.List<ThemeKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ThemeKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ThemeKind._(super.value, super.name);
}

class ThemeInputKind extends $pb.ProtobufEnum {
  static const ThemeInputKind THEME_INPUT_KIND_UNSPECIFIED =
      ThemeInputKind._(0, _omitEnumNames ? '' : 'THEME_INPUT_KIND_UNSPECIFIED');
  static const ThemeInputKind THEME_INPUT_KIND_DARK =
      ThemeInputKind._(1, _omitEnumNames ? '' : 'THEME_INPUT_KIND_DARK');
  static const ThemeInputKind THEME_INPUT_KIND_LIGHT =
      ThemeInputKind._(2, _omitEnumNames ? '' : 'THEME_INPUT_KIND_LIGHT');

  static const $core.List<ThemeInputKind> values = <ThemeInputKind>[
    THEME_INPUT_KIND_UNSPECIFIED,
    THEME_INPUT_KIND_DARK,
    THEME_INPUT_KIND_LIGHT,
  ];

  static final $core.List<ThemeInputKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static ThemeInputKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ThemeInputKind._(super.value, super.name);
}

class SetDefaultThemeRequestKind extends $pb.ProtobufEnum {
  static const SetDefaultThemeRequestKind
      SET_DEFAULT_THEME_REQUEST_KIND_UNSPECIFIED = SetDefaultThemeRequestKind._(
          0,
          _omitEnumNames ? '' : 'SET_DEFAULT_THEME_REQUEST_KIND_UNSPECIFIED');
  static const SetDefaultThemeRequestKind SET_DEFAULT_THEME_REQUEST_KIND_DARK =
      SetDefaultThemeRequestKind._(
          1, _omitEnumNames ? '' : 'SET_DEFAULT_THEME_REQUEST_KIND_DARK');
  static const SetDefaultThemeRequestKind SET_DEFAULT_THEME_REQUEST_KIND_LIGHT =
      SetDefaultThemeRequestKind._(
          2, _omitEnumNames ? '' : 'SET_DEFAULT_THEME_REQUEST_KIND_LIGHT');

  static const $core.List<SetDefaultThemeRequestKind> values =
      <SetDefaultThemeRequestKind>[
    SET_DEFAULT_THEME_REQUEST_KIND_UNSPECIFIED,
    SET_DEFAULT_THEME_REQUEST_KIND_DARK,
    SET_DEFAULT_THEME_REQUEST_KIND_LIGHT,
  ];

  static final $core.List<SetDefaultThemeRequestKind?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static SetDefaultThemeRequestKind? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const SetDefaultThemeRequestKind._(super.value, super.name);
}

class MessageCreatedFrameNotify extends $pb.ProtobufEnum {
  static const MessageCreatedFrameNotify
      MESSAGE_CREATED_FRAME_NOTIFY_UNSPECIFIED = MessageCreatedFrameNotify._(
          0, _omitEnumNames ? '' : 'MESSAGE_CREATED_FRAME_NOTIFY_UNSPECIFIED');
  static const MessageCreatedFrameNotify MESSAGE_CREATED_FRAME_NOTIFY_NONE =
      MessageCreatedFrameNotify._(
          1, _omitEnumNames ? '' : 'MESSAGE_CREATED_FRAME_NOTIFY_NONE');
  static const MessageCreatedFrameNotify MESSAGE_CREATED_FRAME_NOTIFY_BADGE =
      MessageCreatedFrameNotify._(
          2, _omitEnumNames ? '' : 'MESSAGE_CREATED_FRAME_NOTIFY_BADGE');
  static const MessageCreatedFrameNotify MESSAGE_CREATED_FRAME_NOTIFY_NOTIFY =
      MessageCreatedFrameNotify._(
          3, _omitEnumNames ? '' : 'MESSAGE_CREATED_FRAME_NOTIFY_NOTIFY');

  static const $core.List<MessageCreatedFrameNotify> values =
      <MessageCreatedFrameNotify>[
    MESSAGE_CREATED_FRAME_NOTIFY_UNSPECIFIED,
    MESSAGE_CREATED_FRAME_NOTIFY_NONE,
    MESSAGE_CREATED_FRAME_NOTIFY_BADGE,
    MESSAGE_CREATED_FRAME_NOTIFY_NOTIFY,
  ];

  static final $core.List<MessageCreatedFrameNotify?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static MessageCreatedFrameNotify? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MessageCreatedFrameNotify._(super.value, super.name);
}

class PresenceChangedFramePresence extends $pb.ProtobufEnum {
  static const PresenceChangedFramePresence
      PRESENCE_CHANGED_FRAME_PRESENCE_UNSPECIFIED =
      PresenceChangedFramePresence._(0,
          _omitEnumNames ? '' : 'PRESENCE_CHANGED_FRAME_PRESENCE_UNSPECIFIED');
  static const PresenceChangedFramePresence
      PRESENCE_CHANGED_FRAME_PRESENCE_ONLINE = PresenceChangedFramePresence._(
          1, _omitEnumNames ? '' : 'PRESENCE_CHANGED_FRAME_PRESENCE_ONLINE');
  static const PresenceChangedFramePresence
      PRESENCE_CHANGED_FRAME_PRESENCE_AWAY = PresenceChangedFramePresence._(
          2, _omitEnumNames ? '' : 'PRESENCE_CHANGED_FRAME_PRESENCE_AWAY');
  static const PresenceChangedFramePresence
      PRESENCE_CHANGED_FRAME_PRESENCE_OFFLINE = PresenceChangedFramePresence._(
          3, _omitEnumNames ? '' : 'PRESENCE_CHANGED_FRAME_PRESENCE_OFFLINE');

  static const $core.List<PresenceChangedFramePresence> values =
      <PresenceChangedFramePresence>[
    PRESENCE_CHANGED_FRAME_PRESENCE_UNSPECIFIED,
    PRESENCE_CHANGED_FRAME_PRESENCE_ONLINE,
    PRESENCE_CHANGED_FRAME_PRESENCE_AWAY,
    PRESENCE_CHANGED_FRAME_PRESENCE_OFFLINE,
  ];

  static final $core.List<PresenceChangedFramePresence?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static PresenceChangedFramePresence? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PresenceChangedFramePresence._(super.value, super.name);
}

class PresenceChangePresence extends $pb.ProtobufEnum {
  static const PresenceChangePresence PRESENCE_CHANGE_PRESENCE_UNSPECIFIED =
      PresenceChangePresence._(
          0, _omitEnumNames ? '' : 'PRESENCE_CHANGE_PRESENCE_UNSPECIFIED');
  static const PresenceChangePresence PRESENCE_CHANGE_PRESENCE_ONLINE =
      PresenceChangePresence._(
          1, _omitEnumNames ? '' : 'PRESENCE_CHANGE_PRESENCE_ONLINE');
  static const PresenceChangePresence PRESENCE_CHANGE_PRESENCE_AWAY =
      PresenceChangePresence._(
          2, _omitEnumNames ? '' : 'PRESENCE_CHANGE_PRESENCE_AWAY');
  static const PresenceChangePresence PRESENCE_CHANGE_PRESENCE_OFFLINE =
      PresenceChangePresence._(
          3, _omitEnumNames ? '' : 'PRESENCE_CHANGE_PRESENCE_OFFLINE');

  static const $core.List<PresenceChangePresence> values =
      <PresenceChangePresence>[
    PRESENCE_CHANGE_PRESENCE_UNSPECIFIED,
    PRESENCE_CHANGE_PRESENCE_ONLINE,
    PRESENCE_CHANGE_PRESENCE_AWAY,
    PRESENCE_CHANGE_PRESENCE_OFFLINE,
  ];

  static final $core.List<PresenceChangePresence?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static PresenceChangePresence? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const PresenceChangePresence._(super.value, super.name);
}

class ReloadFrameReason extends $pb.ProtobufEnum {
  static const ReloadFrameReason RELOAD_FRAME_REASON_UNSPECIFIED =
      ReloadFrameReason._(
          0, _omitEnumNames ? '' : 'RELOAD_FRAME_REASON_UNSPECIFIED');
  static const ReloadFrameReason RELOAD_FRAME_REASON_TOO_FAR_BEHIND =
      ReloadFrameReason._(
          1, _omitEnumNames ? '' : 'RELOAD_FRAME_REASON_TOO_FAR_BEHIND');
  static const ReloadFrameReason RELOAD_FRAME_REASON_NO_LONGER_MEMBER =
      ReloadFrameReason._(
          2, _omitEnumNames ? '' : 'RELOAD_FRAME_REASON_NO_LONGER_MEMBER');
  static const ReloadFrameReason RELOAD_FRAME_REASON_UNKNOWN =
      ReloadFrameReason._(
          3, _omitEnumNames ? '' : 'RELOAD_FRAME_REASON_UNKNOWN');

  static const $core.List<ReloadFrameReason> values = <ReloadFrameReason>[
    RELOAD_FRAME_REASON_UNSPECIFIED,
    RELOAD_FRAME_REASON_TOO_FAR_BEHIND,
    RELOAD_FRAME_REASON_NO_LONGER_MEMBER,
    RELOAD_FRAME_REASON_UNKNOWN,
  ];

  static final $core.List<ReloadFrameReason?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static ReloadFrameReason? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ReloadFrameReason._(super.value, super.name);
}

class ErrorDetailCode extends $pb.ProtobufEnum {
  static const ErrorDetailCode ERROR_DETAIL_CODE_UNSPECIFIED =
      ErrorDetailCode._(
          0, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_UNSPECIFIED');
  static const ErrorDetailCode ERROR_DETAIL_CODE_INVALID_REQUEST =
      ErrorDetailCode._(
          1, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_INVALID_REQUEST');
  static const ErrorDetailCode ERROR_DETAIL_CODE_INVALID_VALUE =
      ErrorDetailCode._(
          2, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_INVALID_VALUE');
  static const ErrorDetailCode ERROR_DETAIL_CODE_UNAUTHENTICATED =
      ErrorDetailCode._(
          3, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_UNAUTHENTICATED');
  static const ErrorDetailCode ERROR_DETAIL_CODE_SECOND_FACTOR_REQUIRED =
      ErrorDetailCode._(
          4, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_SECOND_FACTOR_REQUIRED');
  static const ErrorDetailCode ERROR_DETAIL_CODE_FORBIDDEN =
      ErrorDetailCode._(5, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_FORBIDDEN');
  static const ErrorDetailCode ERROR_DETAIL_CODE_TIMED_OUT =
      ErrorDetailCode._(6, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_TIMED_OUT');
  static const ErrorDetailCode ERROR_DETAIL_CODE_ARCHIVED =
      ErrorDetailCode._(7, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_ARCHIVED');
  static const ErrorDetailCode ERROR_DETAIL_CODE_LOCKED =
      ErrorDetailCode._(8, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_LOCKED');
  static const ErrorDetailCode ERROR_DETAIL_CODE_FEATURE_OFF =
      ErrorDetailCode._(
          9, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_FEATURE_OFF');
  static const ErrorDetailCode ERROR_DETAIL_CODE_FRESH_CHECK_REQUIRED =
      ErrorDetailCode._(
          10, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_FRESH_CHECK_REQUIRED');
  static const ErrorDetailCode ERROR_DETAIL_CODE_NOT_FOUND = ErrorDetailCode._(
      11, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_NOT_FOUND');
  static const ErrorDetailCode ERROR_DETAIL_CODE_CONFLICT =
      ErrorDetailCode._(12, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_CONFLICT');
  static const ErrorDetailCode ERROR_DETAIL_CODE_NAME_TAKEN = ErrorDetailCode._(
      13, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_NAME_TAKEN');
  static const ErrorDetailCode ERROR_DETAIL_CODE_ALREADY_EXISTS =
      ErrorDetailCode._(
          14, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_ALREADY_EXISTS');
  static const ErrorDetailCode ERROR_DETAIL_CODE_TOO_LARGE = ErrorDetailCode._(
      15, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_TOO_LARGE');
  static const ErrorDetailCode ERROR_DETAIL_CODE_BLOCKED_BY_FILTER =
      ErrorDetailCode._(
          16, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_BLOCKED_BY_FILTER');
  static const ErrorDetailCode ERROR_DETAIL_CODE_RATE_LIMITED =
      ErrorDetailCode._(
          17, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_RATE_LIMITED');
  static const ErrorDetailCode ERROR_DETAIL_CODE_SLOW_MODE = ErrorDetailCode._(
      18, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_SLOW_MODE');
  static const ErrorDetailCode ERROR_DETAIL_CODE_INTERNAL =
      ErrorDetailCode._(19, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_INTERNAL');
  static const ErrorDetailCode ERROR_DETAIL_CODE_UNAVAILABLE =
      ErrorDetailCode._(
          20, _omitEnumNames ? '' : 'ERROR_DETAIL_CODE_UNAVAILABLE');

  static const $core.List<ErrorDetailCode> values = <ErrorDetailCode>[
    ERROR_DETAIL_CODE_UNSPECIFIED,
    ERROR_DETAIL_CODE_INVALID_REQUEST,
    ERROR_DETAIL_CODE_INVALID_VALUE,
    ERROR_DETAIL_CODE_UNAUTHENTICATED,
    ERROR_DETAIL_CODE_SECOND_FACTOR_REQUIRED,
    ERROR_DETAIL_CODE_FORBIDDEN,
    ERROR_DETAIL_CODE_TIMED_OUT,
    ERROR_DETAIL_CODE_ARCHIVED,
    ERROR_DETAIL_CODE_LOCKED,
    ERROR_DETAIL_CODE_FEATURE_OFF,
    ERROR_DETAIL_CODE_FRESH_CHECK_REQUIRED,
    ERROR_DETAIL_CODE_NOT_FOUND,
    ERROR_DETAIL_CODE_CONFLICT,
    ERROR_DETAIL_CODE_NAME_TAKEN,
    ERROR_DETAIL_CODE_ALREADY_EXISTS,
    ERROR_DETAIL_CODE_TOO_LARGE,
    ERROR_DETAIL_CODE_BLOCKED_BY_FILTER,
    ERROR_DETAIL_CODE_RATE_LIMITED,
    ERROR_DETAIL_CODE_SLOW_MODE,
    ERROR_DETAIL_CODE_INTERNAL,
    ERROR_DETAIL_CODE_UNAVAILABLE,
  ];

  static final $core.List<ErrorDetailCode?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 20);
  static ErrorDetailCode? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const ErrorDetailCode._(super.value, super.name);
}

class FormAnswerWithdrawnFrameRefused extends $pb.ProtobufEnum {
  static const FormAnswerWithdrawnFrameRefused
      FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_UNSPECIFIED =
      FormAnswerWithdrawnFrameRefused._(
          0,
          _omitEnumNames
              ? ''
              : 'FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_UNSPECIFIED');
  static const FormAnswerWithdrawnFrameRefused
      FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_CLOSED =
      FormAnswerWithdrawnFrameRefused._(1,
          _omitEnumNames ? '' : 'FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_CLOSED');
  static const FormAnswerWithdrawnFrameRefused
      FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_FULL =
      FormAnswerWithdrawnFrameRefused._(
          2, _omitEnumNames ? '' : 'FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_FULL');
  static const FormAnswerWithdrawnFrameRefused
      FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_INVALID =
      FormAnswerWithdrawnFrameRefused._(3,
          _omitEnumNames ? '' : 'FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_INVALID');

  static const $core.List<FormAnswerWithdrawnFrameRefused> values =
      <FormAnswerWithdrawnFrameRefused>[
    FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_UNSPECIFIED,
    FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_CLOSED,
    FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_FULL,
    FORM_ANSWER_WITHDRAWN_FRAME_REFUSED_INVALID,
  ];

  static final $core.List<FormAnswerWithdrawnFrameRefused?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static FormAnswerWithdrawnFrameRefused? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const FormAnswerWithdrawnFrameRefused._(super.value, super.name);
}

class MemberLeftFrameReason extends $pb.ProtobufEnum {
  static const MemberLeftFrameReason MEMBER_LEFT_FRAME_REASON_UNSPECIFIED =
      MemberLeftFrameReason._(
          0, _omitEnumNames ? '' : 'MEMBER_LEFT_FRAME_REASON_UNSPECIFIED');
  static const MemberLeftFrameReason MEMBER_LEFT_FRAME_REASON_LEFT =
      MemberLeftFrameReason._(
          1, _omitEnumNames ? '' : 'MEMBER_LEFT_FRAME_REASON_LEFT');
  static const MemberLeftFrameReason MEMBER_LEFT_FRAME_REASON_REMOVED =
      MemberLeftFrameReason._(
          2, _omitEnumNames ? '' : 'MEMBER_LEFT_FRAME_REASON_REMOVED');
  static const MemberLeftFrameReason MEMBER_LEFT_FRAME_REASON_BANNED =
      MemberLeftFrameReason._(
          5, _omitEnumNames ? '' : 'MEMBER_LEFT_FRAME_REASON_BANNED');
  static const MemberLeftFrameReason MEMBER_LEFT_FRAME_REASON_GROUP =
      MemberLeftFrameReason._(
          3, _omitEnumNames ? '' : 'MEMBER_LEFT_FRAME_REASON_GROUP');
  static const MemberLeftFrameReason MEMBER_LEFT_FRAME_REASON_ACCOUNT_REMOVED =
      MemberLeftFrameReason._(
          4, _omitEnumNames ? '' : 'MEMBER_LEFT_FRAME_REASON_ACCOUNT_REMOVED');

  static const $core.List<MemberLeftFrameReason> values =
      <MemberLeftFrameReason>[
    MEMBER_LEFT_FRAME_REASON_UNSPECIFIED,
    MEMBER_LEFT_FRAME_REASON_LEFT,
    MEMBER_LEFT_FRAME_REASON_REMOVED,
    MEMBER_LEFT_FRAME_REASON_BANNED,
    MEMBER_LEFT_FRAME_REASON_GROUP,
    MEMBER_LEFT_FRAME_REASON_ACCOUNT_REMOVED,
  ];

  static final $core.List<MemberLeftFrameReason?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static MemberLeftFrameReason? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const MemberLeftFrameReason._(super.value, super.name);
}

class CallEndedFrameReason extends $pb.ProtobufEnum {
  static const CallEndedFrameReason CALL_ENDED_FRAME_REASON_UNSPECIFIED =
      CallEndedFrameReason._(
          0, _omitEnumNames ? '' : 'CALL_ENDED_FRAME_REASON_UNSPECIFIED');
  static const CallEndedFrameReason CALL_ENDED_FRAME_REASON_EMPTY =
      CallEndedFrameReason._(
          1, _omitEnumNames ? '' : 'CALL_ENDED_FRAME_REASON_EMPTY');
  static const CallEndedFrameReason CALL_ENDED_FRAME_REASON_ALONE =
      CallEndedFrameReason._(
          2, _omitEnumNames ? '' : 'CALL_ENDED_FRAME_REASON_ALONE');
  static const CallEndedFrameReason CALL_ENDED_FRAME_REASON_TOO_LONG =
      CallEndedFrameReason._(
          3, _omitEnumNames ? '' : 'CALL_ENDED_FRAME_REASON_TOO_LONG');
  static const CallEndedFrameReason CALL_ENDED_FRAME_REASON_CLOSED =
      CallEndedFrameReason._(
          4, _omitEnumNames ? '' : 'CALL_ENDED_FRAME_REASON_CLOSED');
  static const CallEndedFrameReason CALL_ENDED_FRAME_REASON_SERVER =
      CallEndedFrameReason._(
          5, _omitEnumNames ? '' : 'CALL_ENDED_FRAME_REASON_SERVER');

  static const $core.List<CallEndedFrameReason> values = <CallEndedFrameReason>[
    CALL_ENDED_FRAME_REASON_UNSPECIFIED,
    CALL_ENDED_FRAME_REASON_EMPTY,
    CALL_ENDED_FRAME_REASON_ALONE,
    CALL_ENDED_FRAME_REASON_TOO_LONG,
    CALL_ENDED_FRAME_REASON_CLOSED,
    CALL_ENDED_FRAME_REASON_SERVER,
  ];

  static final $core.List<CallEndedFrameReason?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static CallEndedFrameReason? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallEndedFrameReason._(super.value, super.name);
}

class CallParticipantLeftFrameReason extends $pb.ProtobufEnum {
  static const CallParticipantLeftFrameReason
      CALL_PARTICIPANT_LEFT_FRAME_REASON_UNSPECIFIED =
      CallParticipantLeftFrameReason._(
          0,
          _omitEnumNames
              ? ''
              : 'CALL_PARTICIPANT_LEFT_FRAME_REASON_UNSPECIFIED');
  static const CallParticipantLeftFrameReason
      CALL_PARTICIPANT_LEFT_FRAME_REASON_LEFT =
      CallParticipantLeftFrameReason._(
          1, _omitEnumNames ? '' : 'CALL_PARTICIPANT_LEFT_FRAME_REASON_LEFT');
  static const CallParticipantLeftFrameReason
      CALL_PARTICIPANT_LEFT_FRAME_REASON_TIMEOUT =
      CallParticipantLeftFrameReason._(2,
          _omitEnumNames ? '' : 'CALL_PARTICIPANT_LEFT_FRAME_REASON_TIMEOUT');
  static const CallParticipantLeftFrameReason
      CALL_PARTICIPANT_LEFT_FRAME_REASON_JOINED_ELSEWHERE =
      CallParticipantLeftFrameReason._(
          3,
          _omitEnumNames
              ? ''
              : 'CALL_PARTICIPANT_LEFT_FRAME_REASON_JOINED_ELSEWHERE');
  static const CallParticipantLeftFrameReason
      CALL_PARTICIPANT_LEFT_FRAME_REASON_REMOVED =
      CallParticipantLeftFrameReason._(4,
          _omitEnumNames ? '' : 'CALL_PARTICIPANT_LEFT_FRAME_REASON_REMOVED');
  static const CallParticipantLeftFrameReason
      CALL_PARTICIPANT_LEFT_FRAME_REASON_SIGNED_OUT =
      CallParticipantLeftFrameReason._(
          5,
          _omitEnumNames
              ? ''
              : 'CALL_PARTICIPANT_LEFT_FRAME_REASON_SIGNED_OUT');

  static const $core.List<CallParticipantLeftFrameReason> values =
      <CallParticipantLeftFrameReason>[
    CALL_PARTICIPANT_LEFT_FRAME_REASON_UNSPECIFIED,
    CALL_PARTICIPANT_LEFT_FRAME_REASON_LEFT,
    CALL_PARTICIPANT_LEFT_FRAME_REASON_TIMEOUT,
    CALL_PARTICIPANT_LEFT_FRAME_REASON_JOINED_ELSEWHERE,
    CALL_PARTICIPANT_LEFT_FRAME_REASON_REMOVED,
    CALL_PARTICIPANT_LEFT_FRAME_REASON_SIGNED_OUT,
  ];

  static final $core.List<CallParticipantLeftFrameReason?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 5);
  static CallParticipantLeftFrameReason? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallParticipantLeftFrameReason._(super.value, super.name);
}

class InteractionRespondedFrameResponseType extends $pb.ProtobufEnum {
  static const InteractionRespondedFrameResponseType
      INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_UNSPECIFIED =
      InteractionRespondedFrameResponseType._(
          0,
          _omitEnumNames
              ? ''
              : 'INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_UNSPECIFIED');
  static const InteractionRespondedFrameResponseType
      INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_MESSAGE =
      InteractionRespondedFrameResponseType._(
          1,
          _omitEnumNames
              ? ''
              : 'INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_MESSAGE');
  static const InteractionRespondedFrameResponseType
      INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_PRIVATE =
      InteractionRespondedFrameResponseType._(
          2,
          _omitEnumNames
              ? ''
              : 'INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_PRIVATE');
  static const InteractionRespondedFrameResponseType
      INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_DIALOG =
      InteractionRespondedFrameResponseType._(
          3,
          _omitEnumNames
              ? ''
              : 'INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_DIALOG');
  static const InteractionRespondedFrameResponseType
      INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_NONE =
      InteractionRespondedFrameResponseType._(
          4,
          _omitEnumNames
              ? ''
              : 'INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_NONE');

  static const $core.List<InteractionRespondedFrameResponseType> values =
      <InteractionRespondedFrameResponseType>[
    INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_UNSPECIFIED,
    INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_MESSAGE,
    INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_PRIVATE,
    INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_DIALOG,
    INTERACTION_RESPONDED_FRAME_RESPONSE_TYPE_NONE,
  ];

  static final $core.List<InteractionRespondedFrameResponseType?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static InteractionRespondedFrameResponseType? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const InteractionRespondedFrameResponseType._(super.value, super.name);
}

class CallOfferFramePc extends $pb.ProtobufEnum {
  static const CallOfferFramePc CALL_OFFER_FRAME_PC_UNSPECIFIED =
      CallOfferFramePc._(
          0, _omitEnumNames ? '' : 'CALL_OFFER_FRAME_PC_UNSPECIFIED');
  static const CallOfferFramePc CALL_OFFER_FRAME_PC_SEND =
      CallOfferFramePc._(1, _omitEnumNames ? '' : 'CALL_OFFER_FRAME_PC_SEND');
  static const CallOfferFramePc CALL_OFFER_FRAME_PC_RECV =
      CallOfferFramePc._(2, _omitEnumNames ? '' : 'CALL_OFFER_FRAME_PC_RECV');

  static const $core.List<CallOfferFramePc> values = <CallOfferFramePc>[
    CALL_OFFER_FRAME_PC_UNSPECIFIED,
    CALL_OFFER_FRAME_PC_SEND,
    CALL_OFFER_FRAME_PC_RECV,
  ];

  static final $core.List<CallOfferFramePc?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static CallOfferFramePc? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallOfferFramePc._(super.value, super.name);
}

class CallMediaLineSource extends $pb.ProtobufEnum {
  static const CallMediaLineSource CALL_MEDIA_LINE_SOURCE_UNSPECIFIED =
      CallMediaLineSource._(
          0, _omitEnumNames ? '' : 'CALL_MEDIA_LINE_SOURCE_UNSPECIFIED');
  static const CallMediaLineSource CALL_MEDIA_LINE_SOURCE_MICROPHONE =
      CallMediaLineSource._(
          1, _omitEnumNames ? '' : 'CALL_MEDIA_LINE_SOURCE_MICROPHONE');
  static const CallMediaLineSource CALL_MEDIA_LINE_SOURCE_CAMERA =
      CallMediaLineSource._(
          2, _omitEnumNames ? '' : 'CALL_MEDIA_LINE_SOURCE_CAMERA');
  static const CallMediaLineSource CALL_MEDIA_LINE_SOURCE_SCREEN =
      CallMediaLineSource._(
          3, _omitEnumNames ? '' : 'CALL_MEDIA_LINE_SOURCE_SCREEN');

  static const $core.List<CallMediaLineSource> values = <CallMediaLineSource>[
    CALL_MEDIA_LINE_SOURCE_UNSPECIFIED,
    CALL_MEDIA_LINE_SOURCE_MICROPHONE,
    CALL_MEDIA_LINE_SOURCE_CAMERA,
    CALL_MEDIA_LINE_SOURCE_SCREEN,
  ];

  static final $core.List<CallMediaLineSource?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CallMediaLineSource? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallMediaLineSource._(super.value, super.name);
}

class CallAnswerFramePc extends $pb.ProtobufEnum {
  static const CallAnswerFramePc CALL_ANSWER_FRAME_PC_UNSPECIFIED =
      CallAnswerFramePc._(
          0, _omitEnumNames ? '' : 'CALL_ANSWER_FRAME_PC_UNSPECIFIED');
  static const CallAnswerFramePc CALL_ANSWER_FRAME_PC_SEND =
      CallAnswerFramePc._(1, _omitEnumNames ? '' : 'CALL_ANSWER_FRAME_PC_SEND');
  static const CallAnswerFramePc CALL_ANSWER_FRAME_PC_RECV =
      CallAnswerFramePc._(2, _omitEnumNames ? '' : 'CALL_ANSWER_FRAME_PC_RECV');

  static const $core.List<CallAnswerFramePc> values = <CallAnswerFramePc>[
    CALL_ANSWER_FRAME_PC_UNSPECIFIED,
    CALL_ANSWER_FRAME_PC_SEND,
    CALL_ANSWER_FRAME_PC_RECV,
  ];

  static final $core.List<CallAnswerFramePc?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static CallAnswerFramePc? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallAnswerFramePc._(super.value, super.name);
}

class CallCandidateFramePc extends $pb.ProtobufEnum {
  static const CallCandidateFramePc CALL_CANDIDATE_FRAME_PC_UNSPECIFIED =
      CallCandidateFramePc._(
          0, _omitEnumNames ? '' : 'CALL_CANDIDATE_FRAME_PC_UNSPECIFIED');
  static const CallCandidateFramePc CALL_CANDIDATE_FRAME_PC_SEND =
      CallCandidateFramePc._(
          1, _omitEnumNames ? '' : 'CALL_CANDIDATE_FRAME_PC_SEND');
  static const CallCandidateFramePc CALL_CANDIDATE_FRAME_PC_RECV =
      CallCandidateFramePc._(
          2, _omitEnumNames ? '' : 'CALL_CANDIDATE_FRAME_PC_RECV');

  static const $core.List<CallCandidateFramePc> values = <CallCandidateFramePc>[
    CALL_CANDIDATE_FRAME_PC_UNSPECIFIED,
    CALL_CANDIDATE_FRAME_PC_SEND,
    CALL_CANDIDATE_FRAME_PC_RECV,
  ];

  static final $core.List<CallCandidateFramePc?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static CallCandidateFramePc? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallCandidateFramePc._(super.value, super.name);
}

class CallMuteFrameSource extends $pb.ProtobufEnum {
  static const CallMuteFrameSource CALL_MUTE_FRAME_SOURCE_UNSPECIFIED =
      CallMuteFrameSource._(
          0, _omitEnumNames ? '' : 'CALL_MUTE_FRAME_SOURCE_UNSPECIFIED');
  static const CallMuteFrameSource CALL_MUTE_FRAME_SOURCE_MICROPHONE =
      CallMuteFrameSource._(
          1, _omitEnumNames ? '' : 'CALL_MUTE_FRAME_SOURCE_MICROPHONE');
  static const CallMuteFrameSource CALL_MUTE_FRAME_SOURCE_CAMERA =
      CallMuteFrameSource._(
          2, _omitEnumNames ? '' : 'CALL_MUTE_FRAME_SOURCE_CAMERA');
  static const CallMuteFrameSource CALL_MUTE_FRAME_SOURCE_SCREEN =
      CallMuteFrameSource._(
          3, _omitEnumNames ? '' : 'CALL_MUTE_FRAME_SOURCE_SCREEN');

  static const $core.List<CallMuteFrameSource> values = <CallMuteFrameSource>[
    CALL_MUTE_FRAME_SOURCE_UNSPECIFIED,
    CALL_MUTE_FRAME_SOURCE_MICROPHONE,
    CALL_MUTE_FRAME_SOURCE_CAMERA,
    CALL_MUTE_FRAME_SOURCE_SCREEN,
  ];

  static final $core.List<CallMuteFrameSource?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CallMuteFrameSource? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallMuteFrameSource._(super.value, super.name);
}

class CallRestartFramePc extends $pb.ProtobufEnum {
  static const CallRestartFramePc CALL_RESTART_FRAME_PC_UNSPECIFIED =
      CallRestartFramePc._(
          0, _omitEnumNames ? '' : 'CALL_RESTART_FRAME_PC_UNSPECIFIED');
  static const CallRestartFramePc CALL_RESTART_FRAME_PC_SEND =
      CallRestartFramePc._(
          1, _omitEnumNames ? '' : 'CALL_RESTART_FRAME_PC_SEND');
  static const CallRestartFramePc CALL_RESTART_FRAME_PC_RECV =
      CallRestartFramePc._(
          2, _omitEnumNames ? '' : 'CALL_RESTART_FRAME_PC_RECV');

  static const $core.List<CallRestartFramePc> values = <CallRestartFramePc>[
    CALL_RESTART_FRAME_PC_UNSPECIFIED,
    CALL_RESTART_FRAME_PC_SEND,
    CALL_RESTART_FRAME_PC_RECV,
  ];

  static final $core.List<CallRestartFramePc?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 2);
  static CallRestartFramePc? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallRestartFramePc._(super.value, super.name);
}

class CallOkFrameVideoState extends $pb.ProtobufEnum {
  static const CallOkFrameVideoState CALL_OK_FRAME_VIDEO_STATE_UNSPECIFIED =
      CallOkFrameVideoState._(
          0, _omitEnumNames ? '' : 'CALL_OK_FRAME_VIDEO_STATE_UNSPECIFIED');
  static const CallOkFrameVideoState CALL_OK_FRAME_VIDEO_STATE_FORWARDING =
      CallOkFrameVideoState._(
          1, _omitEnumNames ? '' : 'CALL_OK_FRAME_VIDEO_STATE_FORWARDING');
  static const CallOkFrameVideoState CALL_OK_FRAME_VIDEO_STATE_PAUSED =
      CallOkFrameVideoState._(
          2, _omitEnumNames ? '' : 'CALL_OK_FRAME_VIDEO_STATE_PAUSED');
  static const CallOkFrameVideoState CALL_OK_FRAME_VIDEO_STATE_MUTED =
      CallOkFrameVideoState._(
          3, _omitEnumNames ? '' : 'CALL_OK_FRAME_VIDEO_STATE_MUTED');
  static const CallOkFrameVideoState CALL_OK_FRAME_VIDEO_STATE_UNAVAILABLE =
      CallOkFrameVideoState._(
          4, _omitEnumNames ? '' : 'CALL_OK_FRAME_VIDEO_STATE_UNAVAILABLE');

  static const $core.List<CallOkFrameVideoState> values =
      <CallOkFrameVideoState>[
    CALL_OK_FRAME_VIDEO_STATE_UNSPECIFIED,
    CALL_OK_FRAME_VIDEO_STATE_FORWARDING,
    CALL_OK_FRAME_VIDEO_STATE_PAUSED,
    CALL_OK_FRAME_VIDEO_STATE_MUTED,
    CALL_OK_FRAME_VIDEO_STATE_UNAVAILABLE,
  ];

  static final $core.List<CallOkFrameVideoState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static CallOkFrameVideoState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallOkFrameVideoState._(super.value, super.name);
}

class CallMuteChangedFrameSource extends $pb.ProtobufEnum {
  static const CallMuteChangedFrameSource
      CALL_MUTE_CHANGED_FRAME_SOURCE_UNSPECIFIED = CallMuteChangedFrameSource._(
          0,
          _omitEnumNames ? '' : 'CALL_MUTE_CHANGED_FRAME_SOURCE_UNSPECIFIED');
  static const CallMuteChangedFrameSource
      CALL_MUTE_CHANGED_FRAME_SOURCE_MICROPHONE = CallMuteChangedFrameSource._(
          1, _omitEnumNames ? '' : 'CALL_MUTE_CHANGED_FRAME_SOURCE_MICROPHONE');
  static const CallMuteChangedFrameSource
      CALL_MUTE_CHANGED_FRAME_SOURCE_CAMERA = CallMuteChangedFrameSource._(
          2, _omitEnumNames ? '' : 'CALL_MUTE_CHANGED_FRAME_SOURCE_CAMERA');
  static const CallMuteChangedFrameSource
      CALL_MUTE_CHANGED_FRAME_SOURCE_SCREEN = CallMuteChangedFrameSource._(
          3, _omitEnumNames ? '' : 'CALL_MUTE_CHANGED_FRAME_SOURCE_SCREEN');

  static const $core.List<CallMuteChangedFrameSource> values =
      <CallMuteChangedFrameSource>[
    CALL_MUTE_CHANGED_FRAME_SOURCE_UNSPECIFIED,
    CALL_MUTE_CHANGED_FRAME_SOURCE_MICROPHONE,
    CALL_MUTE_CHANGED_FRAME_SOURCE_CAMERA,
    CALL_MUTE_CHANGED_FRAME_SOURCE_SCREEN,
  ];

  static final $core.List<CallMuteChangedFrameSource?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CallMuteChangedFrameSource? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallMuteChangedFrameSource._(super.value, super.name);
}

class CallLayerChangedFrameState extends $pb.ProtobufEnum {
  static const CallLayerChangedFrameState
      CALL_LAYER_CHANGED_FRAME_STATE_UNSPECIFIED = CallLayerChangedFrameState._(
          0,
          _omitEnumNames ? '' : 'CALL_LAYER_CHANGED_FRAME_STATE_UNSPECIFIED');
  static const CallLayerChangedFrameState
      CALL_LAYER_CHANGED_FRAME_STATE_FORWARDING = CallLayerChangedFrameState._(
          1, _omitEnumNames ? '' : 'CALL_LAYER_CHANGED_FRAME_STATE_FORWARDING');
  static const CallLayerChangedFrameState
      CALL_LAYER_CHANGED_FRAME_STATE_PAUSED = CallLayerChangedFrameState._(
          2, _omitEnumNames ? '' : 'CALL_LAYER_CHANGED_FRAME_STATE_PAUSED');
  static const CallLayerChangedFrameState CALL_LAYER_CHANGED_FRAME_STATE_MUTED =
      CallLayerChangedFrameState._(
          3, _omitEnumNames ? '' : 'CALL_LAYER_CHANGED_FRAME_STATE_MUTED');
  static const CallLayerChangedFrameState
      CALL_LAYER_CHANGED_FRAME_STATE_UNAVAILABLE = CallLayerChangedFrameState._(
          4,
          _omitEnumNames ? '' : 'CALL_LAYER_CHANGED_FRAME_STATE_UNAVAILABLE');

  static const $core.List<CallLayerChangedFrameState> values =
      <CallLayerChangedFrameState>[
    CALL_LAYER_CHANGED_FRAME_STATE_UNSPECIFIED,
    CALL_LAYER_CHANGED_FRAME_STATE_FORWARDING,
    CALL_LAYER_CHANGED_FRAME_STATE_PAUSED,
    CALL_LAYER_CHANGED_FRAME_STATE_MUTED,
    CALL_LAYER_CHANGED_FRAME_STATE_UNAVAILABLE,
  ];

  static final $core.List<CallLayerChangedFrameState?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static CallLayerChangedFrameState? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallLayerChangedFrameState._(super.value, super.name);
}

class CallLayerChangedFrameReason extends $pb.ProtobufEnum {
  static const CallLayerChangedFrameReason
      CALL_LAYER_CHANGED_FRAME_REASON_UNSPECIFIED =
      CallLayerChangedFrameReason._(0,
          _omitEnumNames ? '' : 'CALL_LAYER_CHANGED_FRAME_REASON_UNSPECIFIED');
  static const CallLayerChangedFrameReason
      CALL_LAYER_CHANGED_FRAME_REASON_SIZE = CallLayerChangedFrameReason._(
          1, _omitEnumNames ? '' : 'CALL_LAYER_CHANGED_FRAME_REASON_SIZE');
  static const CallLayerChangedFrameReason
      CALL_LAYER_CHANGED_FRAME_REASON_BANDWIDTH = CallLayerChangedFrameReason._(
          2, _omitEnumNames ? '' : 'CALL_LAYER_CHANGED_FRAME_REASON_BANDWIDTH');
  static const CallLayerChangedFrameReason
      CALL_LAYER_CHANGED_FRAME_REASON_PUBLISHER = CallLayerChangedFrameReason._(
          3, _omitEnumNames ? '' : 'CALL_LAYER_CHANGED_FRAME_REASON_PUBLISHER');

  static const $core.List<CallLayerChangedFrameReason> values =
      <CallLayerChangedFrameReason>[
    CALL_LAYER_CHANGED_FRAME_REASON_UNSPECIFIED,
    CALL_LAYER_CHANGED_FRAME_REASON_SIZE,
    CALL_LAYER_CHANGED_FRAME_REASON_BANDWIDTH,
    CALL_LAYER_CHANGED_FRAME_REASON_PUBLISHER,
  ];

  static final $core.List<CallLayerChangedFrameReason?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static CallLayerChangedFrameReason? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallLayerChangedFrameReason._(super.value, super.name);
}

class CallRingStoppedFrameReason extends $pb.ProtobufEnum {
  static const CallRingStoppedFrameReason
      CALL_RING_STOPPED_FRAME_REASON_UNSPECIFIED = CallRingStoppedFrameReason._(
          0,
          _omitEnumNames ? '' : 'CALL_RING_STOPPED_FRAME_REASON_UNSPECIFIED');
  static const CallRingStoppedFrameReason
      CALL_RING_STOPPED_FRAME_REASON_JOINED_ELSEWHERE =
      CallRingStoppedFrameReason._(
          1,
          _omitEnumNames
              ? ''
              : 'CALL_RING_STOPPED_FRAME_REASON_JOINED_ELSEWHERE');
  static const CallRingStoppedFrameReason
      CALL_RING_STOPPED_FRAME_REASON_DECLINED_ELSEWHERE =
      CallRingStoppedFrameReason._(
          2,
          _omitEnumNames
              ? ''
              : 'CALL_RING_STOPPED_FRAME_REASON_DECLINED_ELSEWHERE');
  static const CallRingStoppedFrameReason CALL_RING_STOPPED_FRAME_REASON_ENDED =
      CallRingStoppedFrameReason._(
          3, _omitEnumNames ? '' : 'CALL_RING_STOPPED_FRAME_REASON_ENDED');
  static const CallRingStoppedFrameReason
      CALL_RING_STOPPED_FRAME_REASON_TIMEOUT = CallRingStoppedFrameReason._(
          4, _omitEnumNames ? '' : 'CALL_RING_STOPPED_FRAME_REASON_TIMEOUT');

  static const $core.List<CallRingStoppedFrameReason> values =
      <CallRingStoppedFrameReason>[
    CALL_RING_STOPPED_FRAME_REASON_UNSPECIFIED,
    CALL_RING_STOPPED_FRAME_REASON_JOINED_ELSEWHERE,
    CALL_RING_STOPPED_FRAME_REASON_DECLINED_ELSEWHERE,
    CALL_RING_STOPPED_FRAME_REASON_ENDED,
    CALL_RING_STOPPED_FRAME_REASON_TIMEOUT,
  ];

  static final $core.List<CallRingStoppedFrameReason?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 4);
  static CallRingStoppedFrameReason? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const CallRingStoppedFrameReason._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
